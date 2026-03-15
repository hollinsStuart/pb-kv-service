package main

import (
	"flag"
	"fmt"

	"github.com/hollinsStuart/pb-kv-service/pbservice"
)

func main() {
	// 1. Define command-line flags
	// We need to know where this node should listen, and where the cluster manager is
	port := flag.String("port", ":50001", "Port for this PBService node to listen on (e.g., :50001)")
	vsAddress := flag.String("vs", "localhost:50000", "Address of the ViewService (e.g., localhost:50000)")
	flag.Parse()

	fmt.Printf("Starting PBService node on port %s...\n", *port)
	fmt.Printf("Targeting ViewService at %s\n", *vsAddress)

	// 2. Call the StartServer function from your pbservice package
	pbservice.StartServer(*port, *vsAddress)

	fmt.Println("PBService node is running. Press Ctrl+C to exit.")

	// 3. Block the main thread indefinitely so the background goroutines keep running
	select {}
}
