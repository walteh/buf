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

	modulev1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1"
	"connectrpc.com/connect"
)

// CreateModules implements modulev1connect.ModuleServiceClient.
func (c *gitCommitServiceClient) CreateModules(ctx context.Context, req *connect.Request[modulev1.CreateModulesRequest]) (*connect.Response[modulev1.CreateModulesResponse], error) {
	// Not implemented for git repositories
	return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("CreateModules not implemented for git repositories"))
}

// DeleteModules implements modulev1connect.ModuleServiceClient.
func (c *gitCommitServiceClient) DeleteModules(ctx context.Context, req *connect.Request[modulev1.DeleteModulesRequest]) (*connect.Response[modulev1.DeleteModulesResponse], error) {
	// Not implemented for git repositories
	return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("DeleteModules not implemented for git repositories"))
}

// ListModules implements modulev1connect.ModuleServiceClient.
func (c *gitCommitServiceClient) ListModules(ctx context.Context, req *connect.Request[modulev1.ListModulesRequest]) (*connect.Response[modulev1.ListModulesResponse], error) {
	// Not implemented for git repositories
	return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("ListModules not implemented for git repositories"))
}

// UpdateModules implements modulev1connect.ModuleServiceClient.
func (c *gitCommitServiceClient) UpdateModules(ctx context.Context, req *connect.Request[modulev1.UpdateModulesRequest]) (*connect.Response[modulev1.UpdateModulesResponse], error) {
	// Not implemented for git repositories
	return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("UpdateModules not implemented for git repositories"))
}

// ArchiveLabels implements modulev1connect.LabelServiceClient.
func (c *gitCommitServiceClient) ArchiveLabels(ctx context.Context, req *connect.Request[modulev1.ArchiveLabelsRequest]) (*connect.Response[modulev1.ArchiveLabelsResponse], error) {
	// Not implemented for git repositories
	return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("ArchiveLabels not implemented for git repositories"))
}

// CreateOrUpdateLabels implements modulev1connect.LabelServiceClient.
func (c *gitCommitServiceClient) CreateOrUpdateLabels(ctx context.Context, req *connect.Request[modulev1.CreateOrUpdateLabelsRequest]) (*connect.Response[modulev1.CreateOrUpdateLabelsResponse], error) {
	// Not implemented for git repositories
	return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("CreateOrUpdateLabels not implemented for git repositories"))
}

// ListLabelHistory implements modulev1connect.LabelServiceClient.
func (c *gitCommitServiceClient) ListLabelHistory(ctx context.Context, req *connect.Request[modulev1.ListLabelHistoryRequest]) (*connect.Response[modulev1.ListLabelHistoryResponse], error) {
	// Not implemented for git repositories
	return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("ListLabelHistory not implemented for git repositories"))
}

// ListLabels implements modulev1connect.LabelServiceClient.
func (c *gitCommitServiceClient) ListLabels(ctx context.Context, req *connect.Request[modulev1.ListLabelsRequest]) (*connect.Response[modulev1.ListLabelsResponse], error) {
	// Not implemented for git repositories
	return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("ListLabels not implemented for git repositories"))
}

// UnarchiveLabels implements modulev1connect.LabelServiceClient.
func (c *gitCommitServiceClient) UnarchiveLabels(ctx context.Context, req *connect.Request[modulev1.UnarchiveLabelsRequest]) (*connect.Response[modulev1.UnarchiveLabelsResponse], error) {
	// Not implemented for git repositories
	return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("UnarchiveLabels not implemented for git repositories"))
}

// GetLabels implements modulev1connect.LabelServiceClient
func (c *gitCommitServiceClient) GetLabels(ctx context.Context, req *connect.Request[modulev1.GetLabelsRequest]) (*connect.Response[modulev1.GetLabelsResponse], error) {
	// Not implemented for git repositories
	return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("GetLabels not implemented for git repositories"))
}

// GetResources implements modulev1connect.ResourceServiceClient
func (c *gitCommitServiceClient) GetResources(ctx context.Context, req *connect.Request[modulev1.GetResourcesRequest]) (*connect.Response[modulev1.GetResourcesResponse], error) {
	// Not implemented for git repositories
	return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("GetResources not implemented for git repositories"))
}

// Upload implements modulev1connect.UploadServiceClient
func (c *gitCommitServiceClient) Upload(ctx context.Context, req *connect.Request[modulev1.UploadRequest]) (*connect.Response[modulev1.UploadResponse], error) {
	// Not implemented for git repositories
	return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("Upload not implemented for git repositories"))
}
