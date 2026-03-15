# Primary-Backup-KV-Service

## Generate go code

Output to `./chatpb/`.

```bash
# Create the output directories
mkdir -p proto/viewpb proto/pbpb

# Compile view.proto
protoc -I=proto \
       --go_out=proto/viewpb --go_opt=paths=source_relative \
       --go-grpc_out=proto/viewpb --go-grpc_opt=paths=source_relative \
       view.proto

# Compile pb.proto
protoc -I=proto \
       --go_out=proto/pbpb --go_opt=paths=source_relative \
       --go-grpc_out=proto/pbpb --go-grpc_opt=paths=source_relative \
       pb.proto
```

## Running the ViewService

The ViewService acts as the central control plane for the distributed key-value store, assigning Primary and Backup roles and monitoring node liveness via gRPC heartbeats. To deploy it as a standalone service, navigate to the root of the project and compile the entry point into an executable binary. Once compiled, you can run the server and specify the TCP port it should listen on.

```bash
# 1. Compile the ViewService into the dedicated bin/ directory
go build -o ./bin/viewserver ./cmd/viewserver

# 2. Run the server (defaults to port :50000 if the flag is omitted)
./bin/viewserver -port :50000
```

## Simple example

```bash
go run ./cmd/viewservice/main.go -port :50000

go run ./cmd/pbservice/main.go -port :50001 -vs localhost:50000

go run ./cmd/pbservice/main.go -port :50002 -vs localhost:50000
```
