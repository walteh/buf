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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"buf.build/gen/go/bufbuild/registry/connectrpc/go/buf/registry/module/v1/modulev1connect"
	modulev1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1"
	"github.com/walteh/buf/private/bufpkg/bufmodule"
	"github.com/walteh/buf/private/pkg/storage"
	"github.com/walteh/buf/private/pkg/storage/storageos"
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
		cmd := exec.CommandContext(ctx, "git", "clone", "https://"+c.gitURL+"/"+owner+"/"+strings.Split(module, "/")[0], "--depth", "1", repoDir)
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
				checkoutCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "checkout", gitRef)
				checkoutCmd.Env = os.Environ()
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

	rbucket := storage.FilterReadBucket(bucket, storage.MatchAnd(storage.MatchOr(storage.MatchPathExt(".proto"), storage.MatchPathEqual(""))))

	return rbucket, nil
}

func (c *gitCommitServiceClient) createDigestFromGitHash(ctx context.Context, repoDir string, internalRepoDir string) (*modulev1.Digest, error) {

	rbucket, err := fileReader(ctx, repoDir, internalRepoDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get files digest: %w", err)
	}

	filesDigest, err := bufmodule.GetB5DigestForBucketAndDepDigests(ctx, rbucket, []bufmodule.Digest{})
	if err != nil {
		return nil, fmt.Errorf("failed to get files digest: %w", err)
	}

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
