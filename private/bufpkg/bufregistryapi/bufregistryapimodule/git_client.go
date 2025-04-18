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
	"fmt"
	"os"
	"os/signal"
	"sync"

	modulev1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1"
	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var commitServiceClientsByRegistry = map[string]*gitCommitServiceClient{}
var commitServiceClientsByRegistryMutex = &sync.Mutex{}

func GetGitServiceClient(gitUrl string) (*gitCommitServiceClient, error) {
	commitServiceClientsByRegistryMutex.Lock()
	defer commitServiceClientsByRegistryMutex.Unlock()

	if c, ok := commitServiceClientsByRegistry[gitUrl]; ok {
		return c, nil
	}

	tmpDir, err := os.MkdirTemp("", "buf-git-repo-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
		os.RemoveAll(tmpDir)
	}()

	c := &gitCommitServiceClient{
		gitURL:     gitUrl,
		workingDir: tmpDir,
	}

	commitServiceClientsByRegistry[gitUrl] = c

	return c, nil
}

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
		// commitID := uuid.New(uuid.NameSpaceOID, []byte(gitCommit.hash))

		// Create a modulev1.Commit
		commit := &modulev1.Commit{
			Id:               gitCommit.hash[:32],
			OwnerId:          owner,
			ModuleId:         module,
			CreateTime:       timestamppb.New(gitCommit.time),
			Digest:           digest,
			CreatedByUserId:  gitCommit.authorEmail,
			SourceControlUrl: "https://github.com/" + owner + "/" + module,
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

// GetGraph implements modulev1connect.GraphServiceClient for git repositories.
// It builds a simple graph of the module and its dependencies based on proto imports.
func (c *gitCommitServiceClient) GetGraph(ctx context.Context, req *connect.Request[modulev1.GetGraphRequest]) (*connect.Response[modulev1.GetGraphResponse], error) {
	// Process module references to extract the needed information
	if len(req.Msg.ResourceRefs) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("at least one module reference is required"))
	}

	commits := []*modulev1.Commit{}

	for _, resourceRef := range req.Msg.ResourceRefs {
		s, ok := seenCommits[resourceRef.GetId()]
		if !ok {
			s = &modulev1.Commit{
				Id: resourceRef.GetId(),
			}
		}

		commits = append(commits, s)
	}

	graph := &modulev1.Graph{
		Commits: commits,
	}

	// Create the response
	return connect.NewResponse(&modulev1.GetGraphResponse{
		Graph: graph,
	}), nil
}

// GetModules implements modulev1connect.ModuleServiceClient
func (c *gitCommitServiceClient) GetModules(ctx context.Context, req *connect.Request[modulev1.GetModulesRequest]) (*connect.Response[modulev1.GetModulesResponse], error) {
	modules := []*modulev1.Module{}
	for _, moduleRef := range req.Msg.GetModuleRefs() {
		modules = append(modules, &modulev1.Module{
			Id:    moduleRef.GetId(),
			State: modulev1.ModuleState_MODULE_STATE_ACTIVE,
		})
	}
	// Not implemented for git repositories
	// just return a nondeprecated dummy module
	return connect.NewResponse(&modulev1.GetModulesResponse{
		Modules: modules,
	}), nil
}

// Download implements modulev1connect.DownloadServiceClient
func (c *gitCommitServiceClient) Download(ctx context.Context, req *connect.Request[modulev1.DownloadRequest]) (*connect.Response[modulev1.DownloadResponse], error) {
	contents := []*modulev1.DownloadResponse_Content{}
	for _, value := range req.Msg.GetValues() {
		commit, ok := seenCommits[value.ResourceRef.GetId()]
		if !ok {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("commit not found: %s", value.ResourceRef.GetId()))
		}
		content := &modulev1.DownloadResponse_Content{
			Commit: commit,
			Files:  []*modulev1.File{},
		}

		repoDir, err := c.ensureRepo(ctx, seenCommitsGitRef[commit.Id], commit.OwnerId, commit.ModuleId)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to ensure repo: %w", err))
		}

		files, err := c.getFiles(ctx, repoDir, commit.ModuleId)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get files: %w", err))
		}

		content.Files = append(content.Files, files...)
		contents = append(contents, content)
	}

	resp := connect.NewResponse(&modulev1.DownloadResponse{
		Contents: contents,
	})

	return resp, nil
}
