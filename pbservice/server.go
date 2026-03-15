package pbservice

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/hollinsStuart/pb-kv-service/proto/pbpb"
	"github.com/hollinsStuart/pb-kv-service/proto/viewpb"
)

// PBServer implements the generated pbpb.PBServiceServer interface
type PBServer struct {
	pbpb.UnimplementedPBServiceServer

	// Use RWMutex so multiple clients can Get() concurrently, but Put() locks everyone
	mu sync.RWMutex

	me string // This node's network address (e.g., "localhost:50001")

	// The actual Key-Value dictionary
	kvStore map[string]string

	// Connection to the cluster manager
	vsClient viewpb.ViewServiceClient
	currView *viewpb.View

	// TODO: We will need a map to track processed client request IDs
	// to ensure "exactly-once" semantics if a client retries a PutAppend!
}

// TransferState is called by the Primary to sync data to a newly joined Backup
func (pb *PBServer) TransferState(ctx context.Context, args *pbpb.TransferStateArgs) (*pbpb.TransferStateReply, error) {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	// 1. We must reject the transfer if we don't think we are the backup
	// (Though usually, if the primary is sending this, we should trust it)
	if pb.currView.Backup != pb.me {
		return &pbpb.TransferStateReply{Err: "Rejecting state: I am not the backup"}, nil
	}

	// 2. Overwrite our local key-value store with the Primary's data
	pb.kvStore = make(map[string]string)
	for k, v := range args.State {
		pb.kvStore[k] = v
	}

	fmt.Printf("[PBServer %s] Accepted state transfer (%d keys populated)\n", pb.me, len(pb.kvStore))

	return &pbpb.TransferStateReply{Err: ""}, nil
}

// --- Initialization ---

// StartServer initializes the PBServer, connects to the ViewService, and starts listening
func StartServer(port string, vsAddress string) *grpc.Server {
	me := fmt.Sprintf("localhost%s", port)

	// 1. Dial the ViewService
	vsConn, err := grpc.Dial(vsAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("PBService %s failed to connect to ViewService at %s: %v", me, vsAddress, err)
	}

	pb := &PBServer{
		me:       me,
		kvStore:  make(map[string]string),
		vsClient: viewpb.NewViewServiceClient(vsConn),
		currView: &viewpb.View{ViewNum: 0},
	}

	// 2. Start the background Ping loop to the ViewService
	go pb.tick()

	// 3. Start the gRPC Server to listen for Clients and other Nodes
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", port, err)
	}

	grpcServer := grpc.NewServer()
	pbpb.RegisterPBServiceServer(grpcServer, pb)

	go func() {
		fmt.Printf("PBService node [%s] listening on %s\n", me, port)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("Failed to serve PBService: %v", err)
		}
	}()

	return grpcServer
}

// tick runs periodically to ping the ViewService and update its current view
func (pb *PBServer) tick() {
	for {
		// 1. Safely get the view number we currently know about
		pb.mu.RLock()
		currentViewNum := pb.currView.ViewNum
		pb.mu.RUnlock()

		// 2. Send the Ping (Heartbeat)
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		reply, err := pb.vsClient.Ping(ctx, &viewpb.PingArgs{
			Me:      pb.me,
			ViewNum: currentViewNum,
		})
		cancel()

		// 3. Process the View if the ViewService responded
		if err == nil && reply != nil && reply.View != nil {
			pb.processView(reply.View)
		}

		time.Sleep(100 * time.Millisecond) // Ping 10 times a second
	}
}

// processView handles role changes and state transfers
func (pb *PBServer) processView(newView *viewpb.View) {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	// If the view hasn't advanced, we have nothing to do
	if newView.ViewNum <= pb.currView.ViewNum {
		return
	}

	// Check our status in this new view
	amPrimary := newView.Primary == pb.me
	hasNewBackup := newView.Backup != "" && newView.Backup != pb.currView.Backup

	// If we are the Primary and there is a brand new Backup, we MUST sync data to it
	if amPrimary && hasNewBackup {
		fmt.Printf("[PBServer %s] Transferring state to new Backup %s...\n", pb.me, newView.Backup)

		// Create a temporary gRPC client to talk to the Backup
		conn, err := grpc.Dial(newView.Backup, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			backupClient := pbpb.NewPBServiceClient(conn)

			// Copy the state map
			stateCopy := make(map[string]string)
			for k, v := range pb.kvStore {
				stateCopy[k] = v
			}

			// Send the data
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, err = backupClient.TransferState(ctx, &pbpb.TransferStateArgs{
				State: stateCopy,
			})
			cancel()
			conn.Close()

			if err == nil {
				fmt.Printf("[PBServer %s] State transfer successful!\n", pb.me)
				pb.currView = newView // SUCCESS: Acknowledge the view!
			} else {
				fmt.Printf("[PBServer %s] State transfer failed: %v\n", pb.me, err)
				// FAILURE: Do NOT update pb.currView. We will try the transfer again on the next tick.
			}
		}
	} else {
		// For all other cases (we are the Backup, or a node just died), just accept the view
		pb.currView = newView
	}
}
