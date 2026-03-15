package pbservice

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/hollinsStuart/pb-kv-service/proto/pbpb"
)

// Get handles client read requests
func (pb *PBServer) Get(ctx context.Context, args *pbpb.GetArgs) (*pbpb.GetReply, error) {
	// RLock allows multiple clients to read concurrently
	pb.mu.RLock()
	defer pb.mu.RUnlock()

	// 1. Reject the request if this node is not the Primary
	if pb.currView.Primary != pb.me {
		return &pbpb.GetReply{Err: "ErrWrongServer: I am not the primary"}, nil
	}

	// 2. Fetch the value
	val, ok := pb.kvStore[args.Key]
	if !ok {
		return &pbpb.GetReply{Err: "ErrNoKey"}, nil
	}

	return &pbpb.GetReply{Value: val, Err: ""}, nil
}

// PutAppend handles client write requests
func (pb *PBServer) PutAppend(ctx context.Context, args *pbpb.PutAppendArgs) (*pbpb.PutAppendReply, error) {
	// Lock prevents reads and other writes while we mutate the data
	pb.mu.Lock()
	defer pb.mu.Unlock()

	// 1. Reject if not Primary
	if pb.currView.Primary != pb.me {
		return &pbpb.PutAppendReply{Err: "ErrWrongServer: I am not the primary"}, nil
	}

	// 2. Forward to Backup (if one exists in the current view)
	if pb.currView.Backup != "" {
		conn, err := grpc.Dial(pb.currView.Backup, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return &pbpb.PutAppendReply{Err: fmt.Sprintf("Failed to connect to backup: %v", err)}, nil
		}
		defer conn.Close()

		backupClient := pbpb.NewPBServiceClient(conn)

		fwdCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		reply, err := backupClient.Forward(fwdCtx, &pbpb.ForwardArgs{Args: args})
		cancel()

		// If replication fails, we abort the whole operation to prevent data divergence
		if err != nil || reply.Err != "" {
			return &pbpb.PutAppendReply{Err: "ErrReplicationFailed"}, nil
		}
	}

	// 3. Apply locally (only after successful replication or if no backup exists)
	pb.applyOp(args)

	return &pbpb.PutAppendReply{Err: ""}, nil
}

// Forward is called by the Primary to replicate a write to the Backup
func (pb *PBServer) Forward(ctx context.Context, args *pbpb.ForwardArgs) (*pbpb.ForwardReply, error) {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	// Reject if this node doesn't think it is the Backup
	if pb.currView.Backup != pb.me {
		return &pbpb.ForwardReply{Err: "ErrWrongServer: I am not the backup"}, nil
	}

	// Apply the operation locally
	pb.applyOp(args.Args)

	return &pbpb.ForwardReply{Err: ""}, nil
}

// applyOp is a private helper function to actually mutate the map
func (pb *PBServer) applyOp(args *pbpb.PutAppendArgs) {
	if args.Op == "Put" {
		pb.kvStore[args.Key] = args.Value
	} else if args.Op == "Append" {
		pb.kvStore[args.Key] += args.Value
	}
}
