// Copyright 2020-2025 Buf Technologies, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bufregistryapimodule

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"buf.build/gen/go/bufbuild/registry/connectrpc/go/buf/registry/module/v1/modulev1connect"
	"buf.build/gen/go/bufbuild/registry/connectrpc/go/buf/registry/module/v1beta1/modulev1beta1connect"
	modulev1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1"
	"connectrpc.com/connect"
	"github.com/bufbuild/buf/private/bufpkg/bufmodule"
	"github.com/bufbuild/buf/private/pkg/storage"
	"github.com/bufbuild/buf/private/pkg/storage/storageos"
	"github.com/bufbuild/buf/private/pkg/uuidutil"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GitURLPrefix is the prefix used to identify a git repository URL
const GitURLPrefix = "github.com"

// gitCacheDirName is the name of the directory where git repositories are cached
const gitCacheDirName = "buf-git-cache"

// maxCacheAge is the maximum age of a cache entry in days
const maxCacheAge = 30

// lastCleanupFile is the name of the file that stores the last cleanup time
const lastCleanupFile = ".last_cleanup"

// cleanupInterval is the minimum interval between cache cleanup operations
const cleanupInterval = 24 * time.Hour

// cacheMutex protects concurrent access to the cache directory
var cacheMutex sync.Mutex

var (
	_ modulev1connect.CommitServiceClient   = &gitCommitServiceClient{}
	_ modulev1connect.DownloadServiceClient = &gitCommitServiceClient{}
	_ modulev1connect.GraphServiceClient    = &gitCommitServiceClient{}
	_ modulev1connect.LabelServiceClient    = &gitCommitServiceClient{}
	_ modulev1connect.ModuleServiceClient   = &gitCommitServiceClient{}
	_ modulev1connect.ResourceServiceClient = &gitCommitServiceClient{}
	_ modulev1connect.UploadServiceClient   = &gitCommitServiceClient{}
)

// gitCommitServiceClient implements the CommitServiceClient interface for git repositories
type gitCommitServiceClient struct {
	gitURL     string
	workingDir string
}

var seenCommits = map[string]*modulev1.Commit{}
var seenCommitsGitRef = map[string]string{}

// ListCommits implements modulev1connect.CommitServiceClient
func (c *gitCommitServiceClient) ListCommits(
	ctx context.Context,
	req *connect.Request[modulev1.ListCommitsRequest],
) (*connect.Response[modulev1.ListCommitsResponse], error) {
	// Not implemented for git repositories
	return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("ListCommits not implemented for git repositories"))
}

// GetCommits implements the CommitServiceClient.GetCommits method to fetch commits from a git repository
func (c *gitCommitServiceClient) GetCommits(
	ctx context.Context,
	req *connect.Request[modulev1.GetCommitsRequest],
) (*connect.Response[modulev1.GetCommitsResponse], error) {
	commits := make([]*modulev1.Commit, 0, len(req.Msg.ResourceRefs))

	for _, resourceRef := range req.Msg.ResourceRefs {
		// Extract name from the proper Value field
		nameValue, ok := resourceRef.Value.(*modulev1.ResourceRef_Name_)
		if !ok || nameValue == nil || nameValue.Name == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("resource ref must have a name value"))
		}
		name := nameValue.Name

		owner := name.Owner
		module := name.Module

		// Extract the git reference (commit hash, branch, or tag)
		gitRef := ""
		if name.Child != nil {
			switch child := name.Child.(type) {
			case *modulev1.ResourceRef_Name_Ref:
				gitRef = child.Ref
			}
		}

		if gitRef == "" {
			gitRef = "main" // Default to main branch if no ref specified
		}

		// Create a temporary directory to clone the repository
		repoDir, err := c.ensureRepo(ctx, gitRef, owner, module)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to prepare git repository: %w", err))
		}

		// Get the commit information
		gitCommit, err := c.getGitCommit(ctx, repoDir, gitRef)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("failed to get git commit: %w", err))
		}

		// Calculate the digest from the files at this commit
		digest, err := c.createDigestFromGitHash(ctx, repoDir, module)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to calculate digest: %w", err))
		}

		// Convert git commit hash to a UUID-like format by hashing it
		commitID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(gitCommit.hash))

		// Create a modulev1.Commit
		commit := &modulev1.Commit{
			Id:         uuidutil.ToDashless(commitID),
			OwnerId:    owner,
			ModuleId:   module,
			CreateTime: timestamppb.New(gitCommit.time),
			Digest:     digest,
		}

		if _, ok := seenCommits[commit.Id]; !ok {
			commits = append(commits, commit)
			seenCommits[commit.Id] = commit
			seenCommitsGitRef[commit.Id] = gitRef
		}
	}

	return connect.NewResponse(&modulev1.GetCommitsResponse{
		Commits: commits,
	}), nil
}

type gitCommitInfo struct {
	hash        string
	time        time.Time
	message     string
	authorName  string
	authorEmail string
}

// ensureRepo ensures the git repository is cloned and up to date for a specific reference
func (c *gitCommitServiceClient) ensureRepo(ctx context.Context, gitRef string, owner string, module string) (string, error) {
	// Get or create the git cache directory
	cacheDir, err := getGitCacheDir()
	if err != nil {
		// If we can't use the cache, fall back to the working directory
		return c.ensureRepoInDir(ctx, c.workingDir, gitRef, owner, module)
	}

	// Create a deterministic directory name based on the git URL and reference
	// This way we cache different versions of the same repository separately
	cacheKey := c.gitURL + ":" + gitRef
	urlHash := sha256.Sum256([]byte(cacheKey))
	repoDirName := hex.EncodeToString(urlHash[:16]) // Use first 16 bytes (32 chars) of hash
	repoDir := filepath.Join(cacheDir, repoDirName)

	return c.ensureRepoInDir(ctx, repoDir, gitRef, owner, module)
}

// ensureRepoInDir ensures the git repository is cloned to the specified directory or updated if it already exists
func (c *gitCommitServiceClient) ensureRepoInDir(ctx context.Context, baseDir string, gitRef string, owner string, module string) (string, error) {
	// Create the base directory if it doesn't exist
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// Update the access time for the cache directory to prevent it from being cleaned up
	if err := touchDir(baseDir); err != nil {
		// Non-fatal error, just log and continue
		fmt.Fprintf(os.Stderr, "Warning: failed to update cache directory access time: %v\n", err)
	}

	repoDir := baseDir
	if filepath.Base(baseDir) != "repo" {
		repoDir = filepath.Join(baseDir, "repo")
	}

	// Check if the repo directory exists
	freshClone := false
	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		// Clone the repository
		cmd := exec.CommandContext(ctx, "git", "clone", "https://"+c.gitURL+"/"+owner+"/"+filepath.SplitList(module)[0], repoDir)
		cmd.Env = os.Environ()
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("failed to clone repository: %w\n%s", err, out)
		}
		freshClone = true
	} else {
		// Update the repository
		cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "fetch", "--all")
		cmd.Env = os.Environ()
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("failed to update repository: %w\n%s", err, out)
		}
	}

	// Checkout the specific reference
	checkoutCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "checkout", gitRef)
	checkoutCmd.Env = os.Environ()
	if out, err := checkoutCmd.CombinedOutput(); err != nil {
		// If we just cloned the repo and can't checkout the ref, try to fetch it specifically
		if freshClone {
			fetchCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "fetch", "origin", gitRef)
			fetchCmd.Env = os.Environ()
			if out, err := fetchCmd.CombinedOutput(); err == nil {
				// Try checkout again after fetch
				if out, err := checkoutCmd.CombinedOutput(); err != nil {
					return "", fmt.Errorf("failed to checkout reference %s after fetch: %w\n%s", gitRef, err, out)
				}
			} else {
				return "", fmt.Errorf("failed to fetch reference %s: %w\n%s", gitRef, err, out)
			}
		} else {
			return "", fmt.Errorf("failed to checkout reference %s: %w\n%s", gitRef, err, out)
		}
	}

	// Make sure we're up to date with the reference
	pullCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "pull", "--ff-only")
	pullCmd.Env = os.Environ()
	// Ignore pull errors as the ref might be a specific commit that can't be pulled
	_ = pullCmd.Run()

	return repoDir, nil
}

// touchDir updates the modification time of a directory to the current time
func touchDir(dirPath string) error {
	currentTime := time.Now().Local()
	return os.Chtimes(dirPath, currentTime, currentTime)
}

// getGitCacheDir returns the directory to use for caching git repositories
func getGitCacheDir() (string, error) {
	// Try to use user cache directory
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}

	cacheDir := filepath.Join(userCacheDir, gitCacheDirName)

	// Create the cache directory if it doesn't exist
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", err
	}

	return cacheDir, nil
}

// getGitCommit gets information about a specific git commit
func (c *gitCommitServiceClient) getGitCommit(ctx context.Context, repoDir, gitRef string) (*gitCommitInfo, error) {
	// First, check out the reference
	checkoutCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "checkout", gitRef)
	checkoutCmd.Env = os.Environ()
	if err := checkoutCmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to checkout reference %s: %w", gitRef, err)
	}

	// Get the commit hash
	hashCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "rev-parse", "HEAD")
	hashCmd.Env = os.Environ()
	hashOutput, err := hashCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get commit hash: %w", err)
	}
	hash := strings.TrimSpace(string(hashOutput))

	// Get the commit time
	timeCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "show", "-s", "--format=%ct", hash)
	timeCmd.Env = os.Environ()
	timeOutput, err := timeCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get commit time: %w", err)
	}

	// Get the commit message
	messageCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "show", "-s", "--format=%B", hash)
	messageCmd.Env = os.Environ()
	messageOutput, err := messageCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get commit message: %w", err)
	}

	// Get the commit author name
	authorNameCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "show", "-s", "--format=%an", hash)
	authorNameCmd.Env = os.Environ()
	authorNameOutput, err := authorNameCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get commit author name: %w", err)
	}

	// Get the commit author email
	authorEmailCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "show", "-s", "--format=%ae", hash)
	authorEmailCmd.Env = os.Environ()
	authorEmailOutput, err := authorEmailCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get commit author email: %w", err)
	}

	// Parse the timestamp (Unix timestamp)
	timestamp := strings.TrimSpace(string(timeOutput))
	unixSeconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		// If we can't parse the timestamp, use current time
		return &gitCommitInfo{
			hash:        hash,
			time:        time.Now(),
			message:     string(messageOutput),
			authorName:  string(authorNameOutput),
			authorEmail: string(authorEmailOutput),
		}, nil
	}

	return &gitCommitInfo{
		hash:        hash,
		time:        time.Unix(unixSeconds, 0),
		message:     string(messageOutput),
		authorName:  string(authorNameOutput),
		authorEmail: string(authorEmailOutput),
	}, nil
}

// func createDigestFromGitHash(gitHash string) (*modulev1.Digest, error) {
// 	// hash := sha256.Sum256([]byte(gitHash))
// 	// return &modulev1.Digest{
// 	// 	Value: []byte(fmt.Sprintf("b5:%x", hash)),
// 	// 	Type:  modulev1.DigestType_DIGEST_TYPE_B5,
// 	// }, nil
// }

func fileReader(ctx context.Context, repoDir string, internalRepoDir string) (storage.ReadBucket, error) {
	storageProvider := storageos.NewProvider(storageos.ProviderWithSymlinks())

	split := strings.Split(internalRepoDir, "/")
	firstDir := ""
	if len(split) > 1 {
		firstDir = strings.Join(split[1:], "/")
	}

	bucket, err := storageProvider.NewReadWriteBucket(filepath.Join(repoDir, firstDir))
	if err != nil {
		return nil, fmt.Errorf("failed to create storage bucket: %w", err)
	}

	fmt.Printf("firstDir: '%s' split: %v\n", firstDir, split)

	rbucket := storage.FilterReadBucket(bucket, storage.MatchAnd(storage.MatchOr(storage.MatchPathExt(".proto"), storage.MatchPathEqual(""))))

	return rbucket, nil
}

func (c *gitCommitServiceClient) createDigestFromGitHash(ctx context.Context, repoDir string, internalRepoDir string) (*modulev1.Digest, error) {
	// digest, err := c.calculateDigest(ctx, repoDir, gitHash)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to calculate digest: %w", err)
	// }
	// return &modulev1.Digest{
	// 	Value: []byte(strings.TrimPrefix(digest, "b5:")),
	// 	Type:  modulev1.DigestType_DIGEST_TYPE_B5,
	// }, nil

	rbucket, err := fileReader(ctx, repoDir, internalRepoDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get files digest: %w", err)
	}
	filesDigest, err := bufmodule.GetB5DigestForBucketAndDepDigests(ctx, rbucket, []bufmodule.Digest{})
	if err != nil {
		return nil, fmt.Errorf("failed to get files digest: %w", err)
	}

	// digest, err := bufmodule.NewDigest(bufmodule.DigestTypeB5, filesDigest)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to create digest: %w", err)
	// }

	return &modulev1.Digest{
		Value: filesDigest.Value(),
		Type:  modulev1.DigestType_DIGEST_TYPE_B5,
	}, nil
}

func (c *gitCommitServiceClient) getFiles(ctx context.Context, localRepoDir, internalRepoDir string) ([]*modulev1.File, error) {
	files := []*modulev1.File{}

	rbucket, err := fileReader(ctx, localRepoDir, internalRepoDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get files digest: %w", err)
	}

	walkFunc := func(objectInfo storage.ObjectInfo) error {
		if strings.HasSuffix(objectInfo.Path(), ".proto") {
			content, err := os.ReadFile(objectInfo.LocalPath())
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			files = append(files, &modulev1.File{
				Path:    objectInfo.Path(),
				Content: content,
			})
		}
		return nil
	}

	err = rbucket.Walk(ctx, "", walkFunc)
	if err != nil {
		return nil, fmt.Errorf("failed to walk repository: %w", err)
	}

	return files, nil
}

// calculateDigest calculates a digest for the files at a specific commit
func (c *gitCommitServiceClient) calculateDigest(ctx context.Context, repoDir, gitHash string) (string, error) {
	// Create a storage provider for the repo directory
	storageProvider := storageos.NewProvider(storageos.ProviderWithSymlinks())
	readWriteBucket, err := storageProvider.NewReadWriteBucket(repoDir)
	if err != nil {
		return "", fmt.Errorf("failed to create storage bucket: %w", err)
	}

	// Get all proto files and calculate a digest
	var paths []string
	walkFunc := func(objectInfo storage.ObjectInfo) error {
		if strings.HasSuffix(objectInfo.Path(), ".proto") {
			paths = append(paths, objectInfo.Path())
		}
		return nil
	}

	err = readWriteBucket.Walk(ctx, "", walkFunc)
	if err != nil {
		return "", fmt.Errorf("failed to walk repository: %w", err)
	}

	// For now, use a simple hash of all proto file paths as the digest
	// In a real implementation, this should use the same digest algorithm as the rest of the system
	digestString := strings.Join(paths, ";") + ":" + gitHash

	// Use SHA256 for the digest computation
	hash := sha256.Sum256([]byte(digestString))
	return fmt.Sprintf("b5:%x", hash), nil
}

// cleanupGitCache removes old cache entries that haven't been accessed in a while
func cleanupGitCache(ctx context.Context) error {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	// Get the cache directory
	cacheDir, err := getGitCacheDir()
	if err != nil {
		return err
	}

	// Check when we last ran a cleanup
	lastCleanupPath := filepath.Join(cacheDir, lastCleanupFile)
	lastCleanupTime := time.Time{}

	if data, err := os.ReadFile(lastCleanupPath); err == nil {
		if timestamp, err := strconv.ParseInt(string(data), 10, 64); err == nil {
			lastCleanupTime = time.Unix(timestamp, 0)
		}
	}

	// If we've cleaned up recently, skip it
	if time.Since(lastCleanupTime) < cleanupInterval {
		return nil
	}

	// Update the last cleanup time
	now := time.Now()
	if err := os.WriteFile(lastCleanupPath, []byte(strconv.FormatInt(now.Unix(), 10)), 0644); err != nil {
		return err
	}

	// Get the cutoff time
	cutoffTime := now.AddDate(0, 0, -maxCacheAge)

	// Walk the cache directory and remove old entries
	return filepath.WalkDir(cacheDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Skip the cache dir itself and the last cleanup file
		if path == cacheDir || filepath.Base(path) == lastCleanupFile {
			return nil
		}

		// Only process directories
		if !d.IsDir() {
			return nil
		}

		// Skip if we encounter an error getting file info
		info, err := d.Info()
		if err != nil {
			return nil
		}

		// Skip subdirectories (only process top-level dirs)
		if filepath.Dir(path) != cacheDir {
			return filepath.SkipDir
		}

		// Check if the directory is old enough to be removed
		if info.ModTime().Before(cutoffTime) {
			// Remove the directory
			return os.RemoveAll(path)
		}

		return nil
	})
}

// NewGitCommitServiceClient creates a new CommitServiceClient that works with git repositories
func NewGitCommitServiceClient(gitURL string, workingDir string) modulev1connect.CommitServiceClient {
	// Strip the git+ prefix if it's present
	// normalizedGitURL := strings.TrimPrefix(gitURL, GitURLPrefix)

	// Check if we need to clean up the cache (don't block on this)
	go func() {
		// Use a background context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		// Try to clean up the cache, but don't fail if we can't
		_ = cleanupGitCache(ctx)
	}()

	return &gitCommitServiceClient{
		gitURL:     gitURL,
		workingDir: workingDir,
	}
}

// NewGitDownloadServiceClient creates a DownloadServiceClient that works with git repositories
func NewGitDownloadServiceClient(gitURL string, workingDir string) modulev1connect.DownloadServiceClient {
	return NewGitCommitServiceClient(gitURL, workingDir).(modulev1connect.DownloadServiceClient)
}

// NewGitGraphServiceClient creates a GraphServiceClient that works with git repositories
func NewGitGraphServiceClient(gitURL string, workingDir string) modulev1connect.GraphServiceClient {
	return NewGitCommitServiceClient(gitURL, workingDir).(modulev1connect.GraphServiceClient)
}

// NewGitLabelServiceClient creates a LabelServiceClient that works with git repositories
func NewGitLabelServiceClient(gitURL string, workingDir string) modulev1connect.LabelServiceClient {
	return NewGitCommitServiceClient(gitURL, workingDir).(modulev1connect.LabelServiceClient)
}

// NewGitModuleServiceClient creates a ModuleServiceClient that works with git repositories
func NewGitModuleServiceClient(gitURL string, workingDir string) modulev1connect.ModuleServiceClient {
	return NewGitCommitServiceClient(gitURL, workingDir).(modulev1connect.ModuleServiceClient)
}

// NewGitResourceServiceClient creates a ResourceServiceClient that works with git repositories
func NewGitResourceServiceClient(gitURL string, workingDir string) modulev1connect.ResourceServiceClient {
	return NewGitCommitServiceClient(gitURL, workingDir).(modulev1connect.ResourceServiceClient)
}

// NewGitUploadServiceClient creates an UploadServiceClient that works with git repositories
func NewGitUploadServiceClient(gitURL string, workingDir string) modulev1connect.UploadServiceClient {
	return NewGitCommitServiceClient(gitURL, workingDir).(modulev1connect.UploadServiceClient)
}

// NewGitV1Beta1CommitServiceClient creates a v1beta1 CommitServiceClient that works with git repositories
func NewGitV1Beta1CommitServiceClient(gitURL string, workingDir string) modulev1beta1connect.CommitServiceClient {
	// Note: This would fail at runtime since we're not implementing the beta1 interfaces yet
	// In a real implementation, we would need to create a wrapper client or implement these interfaces

	// Return an unimplemented handler instead to avoid runtime panics
	return modulev1beta1connect.UnimplementedCommitServiceHandler{}
}

// NewGitV1Beta1DownloadServiceClient creates a v1beta1 DownloadServiceClient that works with git repositories
func NewGitV1Beta1DownloadServiceClient(gitURL string, workingDir string) modulev1beta1connect.DownloadServiceClient {
	// Return an unimplemented handler
	return modulev1beta1connect.UnimplementedDownloadServiceHandler{}
}

// NewGitV1Beta1GraphServiceClient creates a v1beta1 GraphServiceClient that works with git repositories
func NewGitV1Beta1GraphServiceClient(gitURL string, workingDir string) modulev1beta1connect.GraphServiceClient {
	// Return an unimplemented handler
	return modulev1beta1connect.UnimplementedGraphServiceHandler{}
}

// NewGitV1Beta1LabelServiceClient creates a v1beta1 LabelServiceClient that works with git repositories
func NewGitV1Beta1LabelServiceClient(gitURL string, workingDir string) modulev1beta1connect.LabelServiceClient {
	// Return an unimplemented handler
	return modulev1beta1connect.UnimplementedLabelServiceHandler{}
}

// NewGitV1Beta1ModuleServiceClient creates a v1beta1 ModuleServiceClient that works with git repositories
func NewGitV1Beta1ModuleServiceClient(gitURL string, workingDir string) modulev1beta1connect.ModuleServiceClient {
	// Return an unimplemented handler
	return modulev1beta1connect.UnimplementedModuleServiceHandler{}
}

// NewGitV1Beta1UploadServiceClient creates a v1beta1 UploadServiceClient that works with git repositories
func NewGitV1Beta1UploadServiceClient(gitURL string, workingDir string) modulev1beta1connect.UploadServiceClient {
	// Return an unimplemented handler
	return modulev1beta1connect.UnimplementedUploadServiceHandler{}
}
