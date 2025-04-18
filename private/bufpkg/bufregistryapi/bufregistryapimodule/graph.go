package bufregistryapimodule

import (
	"context"
	"fmt"

	modulev1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1"
	"connectrpc.com/connect"
)

// GetGraph implements modulev1connect.GraphServiceClient for git repositories.
// It builds a simple graph of the module and its dependencies based on proto imports.
func (c *gitCommitServiceClient) GetGraph(ctx context.Context, req *connect.Request[modulev1.GetGraphRequest]) (*connect.Response[modulev1.GetGraphResponse], error) {
	// Process module references to extract the needed information
	if len(req.Msg.ResourceRefs) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("at least one module reference is required"))
	}

	fmt.Println("req.Msg.ResourceRefs", req.Msg.ResourceRefs)

	commits := []*modulev1.Commit{}

	for _, resourceRef := range req.Msg.ResourceRefs {
		// repoDir, err := c.ensureRepo(ctx, resourceRef.GetId(), resourceRef.GetName().Owner, resourceRef.GetName().Module)
		// if err != nil {
		// 	return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to ensure repo: %w", err))
		// }

		// cmt, err := c.getGitCommit(ctx, repoDir, resourceRef.GetId())
		// if err != nil {
		// 	return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get git commit: %w", err))
		// }
		// digest, err := c.createDigestFromGitHash(ctx, repoDir, resourceRef.GetId())
		// if err != nil {
		// 	return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create digest: %w", err))
		// }
		commits = append(commits, &modulev1.Commit{
			Id: resourceRef.GetId(),
			// OwnerId:          resourceRef.GetName().Owner,
			// ModuleId:         resourceRef.GetName().Module,
			// CreateTime:       timestamppb.New(cmt.time),
			// Digest:           digest,
			// CreatedByUserId:  cmt.authorEmail,
			// SourceControlUrl: fmt.Sprintf("https://github.com/%s/%s", resourceRef.GetName().Owner, resourceRef.GetName().Module),
		})
	}

	graph := &modulev1.Graph{
		Commits: commits,
	}

	// for _, resourceRef := range req.Msg.ResourceRefs {
	// 	modRef := resourceRef
	// 	owner := modRef.GetName().Owner
	// 	module := modRef.GetName().Module
	// 	reference := modRef.GetId()

	// 	if reference == "" {
	// 		reference = "main" // Default to main branch if no reference specified
	// 	}

	// 	// Use the existing ensureRepo method to get the repository
	// 	repoDir, err := c.ensureRepo(ctx, reference, owner, module)
	// 	if err != nil {
	// 		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to prepare git repository: %w", err))
	// 	}

	// 	// Create a storage provider for reading files from the repo
	// 	storageProvider := storageos.NewProvider(storageos.ProviderWithSymlinks())
	// 	readBucket, err := storageProvider.NewReadWriteBucket(repoDir)
	// 	if err != nil {
	// 		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create storage bucket: %w", err))
	// 	}

	// 	// Find all proto files to analyze
	// 	var protoFiles []string
	// 	err = readBucket.Walk(ctx, "", func(objectInfo storage.ObjectInfo) error {
	// 		if strings.HasSuffix(objectInfo.Path(), ".proto") {
	// 			protoFiles = append(protoFiles, objectInfo.Path())
	// 		}
	// 		return nil
	// 	})
	// 	if err != nil {
	// 		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to find proto files: %w", err))
	// 	}
	// }

	// Build a simple module dependency graph
	// For simplicity, we just represent the repository as a single node
	// nodes := []*modulev1.Graph_Edge{
	// 	{
	// 		FromNode: &modulev1.Graph_Node{
	// 			CommitId: reference,
	// 		},
	// 		ToNode: &modulev1.Graph_Node{
	// 			CommitId: reference,
	// 		},
	// 	},
	// }

	// // Add basic information about the proto files
	// if len(protoFiles) > 0 {
	// 	nodes[0].Info = fmt.Sprintf("Repository contains %d proto files", len(protoFiles))
	// }

	// Create the response
	return connect.NewResponse(&modulev1.GetGraphResponse{
		Graph: graph,
	}), nil
}
