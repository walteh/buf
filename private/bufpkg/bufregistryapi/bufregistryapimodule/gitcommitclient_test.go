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
	"os"
	"testing"

	modulev1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
)

func TestGitCommitServiceClient(t *testing.T) {
	// Skip if not a live test environment or no git
	if os.Getenv("BUF_LIVE_TESTS") != "1" {
		t.Skip("Skipping git client test - enable with BUF_LIVE_TESTS=1")
	}

	// Create temporary dir for test
	tempDir, err := os.MkdirTemp("", "git-commit-client-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create client with a sample git repo URL
	// Using buf's own repo for testing
	gitURL := "git+https://github.com/bufbuild/buf.git"
	client := NewGitCommitServiceClient(gitURL, tempDir)
	require.NotNil(t, client)

	// Create a simple resource ref
	req := connect.NewRequest(&modulev1.GetCommitsRequest{
		ResourceRefs: []*modulev1.ResourceRef{
			{
				Value: &modulev1.ResourceRef_Name_{
					Name: &modulev1.ResourceRef_Name{
						Owner:  "bufbuild",
						Module: "buf",
						Child: &modulev1.ResourceRef_Name_Ref{
							Ref: "main",
						},
					},
				},
			},
		},
	})

	// Execute the request
	resp, err := client.GetCommits(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.Msg.Commits)

	// Verify the first commit has expected fields
	commit := resp.Msg.Commits[0]
	require.NotEmpty(t, commit.Id)
	require.NotEmpty(t, commit.OwnerId)
	require.NotEmpty(t, commit.ModuleId)
	require.NotNil(t, commit.CreateTime)
	require.NotNil(t, commit.Digest)
	require.Equal(t, modulev1.DigestType_DIGEST_TYPE_B5, commit.Digest.Type)
	require.NotEmpty(t, commit.Digest.Value)
}

func TestGitCommitClientWithClientProvider(t *testing.T) {
	// Skip if not a live test environment or no git
	if os.Getenv("BUF_LIVE_TESTS") != "1" {
		t.Skip("Skipping git client provider test - enable with BUF_LIVE_TESTS=1")
	}

	// Create a provider
	provider := newClientProvider(nil)
	require.NotNil(t, provider)

	// Request a git URL
	gitURL := "git+https://github.com/bufbuild/buf.git"
	client := provider.V1CommitServiceClient(gitURL)
	require.NotNil(t, client)

	// Verify we got a non-nil client of the right type
	_, ok := client.(*gitCommitServiceClient)
	require.True(t, ok, "Expected a git commit service client")
}
