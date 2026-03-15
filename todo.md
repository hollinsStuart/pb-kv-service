# TODO: Rewrite EECS 491 Primary/Backup KV Service Using gRPC

(ref. original `viewservice` + `pbservice` spec) :contentReference[oaicite:0]{index=0}

---

## 1. Initialize the Repository & Go Module

- [ ] `mkdir eecs491-p2-grpc && cd eecs491-p2-grpc`
- [ ] `git init`
- [ ] Initialize Go module:
  - [ ] `go mod init github.com/<your-gh-username>/eecs491-p2-grpc`
- [ ] Create basic directory structure:
  - [ ] `mkdir -p proto`
  - [ ] `mkdir -p viewservice pbservice`
  - [ ] `mkdir -p pkg/kv` # shared KV logic
  - [ ] `mkdir -p internal/testutil`
  - [ ] `mkdir -p cmd/viewserver cmd/pbserver`
  - [ ] `mkdir -p .github/workflows`

---

## 2. Install gRPC & Protobuf Tooling

- [ ] Add dependencies to `go.mod`:
  - [ ] `google.golang.org/grpc`
  - [ ] `google.golang.org/protobuf`
- [ ] Install code generators (local dev machine):
  - [ ] `go install google.golang.org/protobuf/cmd/protoc-gen-go@latest`
  - [ ] `go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest`
- [ ] Ensure `$GOPATH/bin` is on `$PATH` (or `$HOME/go/bin`).

---

## 3. Design & Write `.proto` Files

### 3.1 Viewservice Proto

- [ ] Create `proto/viewservice.proto` with:
  - [ ] `service ViewService { rpc Ping(PingRequest) returns (PingReply); rpc GetView(GetViewRequest) returns (GetViewReply); }`
  - [ ] Messages for:
    - [ ] `PingRequest { string server_id; uint64 viewnum; }`
    - [ ] `PingReply { View view; }`
    - [ ] `View { string primary; string backup; uint64 viewnum; }`
    - [ ] `GetViewRequest {}` / `GetViewReply { View view; }`

### 3.2 PBService Proto (KV + Primary/Backup RPCs)

- [ ] Create `proto/pbservice.proto` with:
  - [ ] `service PBService { rpc Get(GetRequest) returns (GetReply); rpc Put(PutRequest) returns (PutReply); rpc Append(AppendRequest) returns (AppendReply); }`
  - [ ] `service InternalPBService { rpc ReplicateOp(ReplicateOpRequest) returns (ReplicateOpReply); rpc InstallSnapshot(InstallSnapshotRequest) returns (InstallSnapshotReply); }`
  - [ ] Messages:
    - [ ] `GetRequest { string key; string client_id; uint64 request_id; }`
    - [ ] `GetReply { string value; string err; }`
    - [ ] `PutRequest { string key; string value; string client_id; uint64 request_id; }`
    - [ ] `AppendRequest { string key; string arg; string client_id; uint64 request_id; }`
    - [ ] `PutReply / AppendReply { string err; }`
    - [ ] `ReplicateOpRequest { string op_type; string key; string value; string client_id; uint64 request_id; }`
    - [ ] `ReplicateOpReply { string err; }`
    - [ ] `InstallSnapshotRequest { map<string,string> kv; /* plus dedup state if needed */ }`
    - [ ] `InstallSnapshotReply { string err; }`

---

## 4. Generate gRPC Stubs

- [ ] Add a `Makefile` or `go generate` script, e.g. `make proto`:
  - [ ] `protoc --go_out=. --go-grpc_out=. proto/viewservice.proto`
  - [ ] `protoc --go_out=. --go-grpc_out=. proto/pbservice.proto`
- [ ] Confirm generated layout:
  - [ ] `proto/viewservice.pb.go`
  - [ ] `proto/viewservice_grpc.pb.go`
  - [ ] `proto/pbservice.pb.go`
  - [ ] `proto/pbservice_grpc.pb.go`

---

## 5. Implement Shared KV Logic (`pkg/kv`)

- [ ] Create `pkg/kv/store.go`:
  - [ ] Type `Store` with:
    - [ ] `mu sync.Mutex`
    - [ ] `data map[string]string`
    - [ ] `dedup map[string]map[uint64]CachedResult` (for at-most-once semantics)
  - [ ] Methods:
    - [ ] `Get(key string) (string, bool)`
    - [ ] `Put(key, value string)`
    - [ ] `Append(key, arg string)`
    - [ ] `ApplyOp(op Op) (Result, error)` with dedup
    - [ ] `Snapshot() (map[string]string, DedupState)` / `Restore(...)`
- [ ] Add `pkg/kv/types.go`:
  - [ ] `type OpType string` (`Get`, `Put`, `Append`)
  - [ ] `type Op struct { OpType; Key; Value; ClientID; RequestID }`
  - [ ] `type Result struct { Value string }`
- [ ] Unit tests in `pkg/kv/store_test.go`.

---

## 6. Implement Viewservice (gRPC)

### 6.1 Server Implementation

- [ ] In `viewservice/server_impl.go`:
  - [ ] Define `type Server struct { proto.UnimplementedViewServiceServer; mu; currentView; lastPing map[serverID]int; ackedView uint64; dead <-chan struct{}; }`
  - [ ] Implement `Ping(ctx, *PingRequest) (*PingReply, error)`:
    - [ ] Track pings, detect new servers, detect restarts (viewnum == 0).
    - [ ] Advance view numbers according to rules from original project.
  - [ ] Implement `GetView(ctx, *GetViewRequest) (*GetViewReply, error)`.
  - [ ] Implement `tick()` goroutine:
    - [ ] Periodically check `lastPing` against `DeadPings` to detect dead servers.
    - [ ] Update primary/backup view accordingly.

### 6.2 gRPC Server Bootstrap

- [ ] In `cmd/viewserver/main.go`:
  - [ ] Parse flags: `-port` or `-listen_addr`.
  - [ ] `lis, _ := net.Listen("tcp", addr)`
  - [ ] `grpcServer := grpc.NewServer()`
  - [ ] `viewservice.RegisterViewServiceServer(grpcServer, newServer(...))`
  - [ ] `grpcServer.Serve(lis)`

### 6.3 Viewservice Client Helper

- [ ] In `viewservice/client.go`:
  - [ ] `type Clerk struct { conn *grpc.ClientConn; cli proto.ViewServiceClient }`
  - [ ] `func NewClerk(addr string) *Clerk`
  - [ ] `func (ck *Clerk) Ping(...)` and `GetView(...)` wrappers with retries and context timeouts.

### 6.4 Tests

- [ ] Port original `viewservice_test.go` logic into:
  - [ ] `viewservice/server_test.go`
- [ ] Use:
  - [ ] In tests, start real gRPC server in-process on random port.
  - [ ] Use Clerk to send Pings and assert view transitions.

---

## 7. Implement PBService (gRPC KV + Primary/Backup)

### 7.1 PBService Server Structure

- [ ] In `pbservice/server_impl.go`:
  - [ ] `type Server struct { proto.UnimplementedPBServiceServer; proto.UnimplementedInternalPBServiceServer; mu; kv *kv.Store; vs *viewservice.Clerk; currentView View; me string; role string; dead <-chan struct{}; }`
  - [ ] `tick()`:
    - [ ] Periodically call `vs.Ping(...)` gRPC.
    - [ ] Update `currentView`, detect if self is primary/backup/idle.
    - [ ] When becoming backup, request snapshot from primary (via `InstallSnapshot`).

### 7.2 Client-Facing RPCs

- [ ] Implement `Get(ctx, *GetRequest) (*GetReply, error)`:
  - [ ] Reject if `!isPrimary()`: return `ErrWrongServer`.
  - [ ] Use KV store with dedup for at-most-once.
- [ ] Implement `Put(ctx, *PutRequest) (*PutReply, error)` & `Append(...)`:
  - [ ] Check role is primary.
  - [ ] Forward op to current backup via InternalPBService `ReplicateOp`:
    - [ ] If backup exists:
      - [ ] Send gRPC with timeout & retry logic.
      - [ ] Decide commit policy: commit only when backup ACKs.
  - [ ] Apply op to local KV store.

### 7.3 Internal Replication RPCs

- [ ] Implement `ReplicateOp(ctx, *ReplicateOpRequest) (*ReplicateOpReply, error)`:
  - [ ] Only accept if `isBackup()`.
  - [ ] Apply op with dedup.
- [ ] Implement `InstallSnapshot(ctx, *InstallSnapshotRequest) (*InstallSnapshotReply, error)`:
  - [ ] Replace local KV map & dedup state.

### 7.4 PBService Clerk (Client Library)

- [ ] In `pbservice/client.go`:
  - [ ] `type Clerk struct { vs *viewservice.Clerk; currentPrimary string; clientID string; nextReqID uint64 }`
  - [ ] `Get(key string) string`
  - [ ] `Put(key, value string)`
  - [ ] `Append(key, arg string)`
  - [ ] Retry loop:
    - [ ] Dial `currentPrimary` via gRPC; on `ErrWrongServer` or RPC failure, query ViewService for new primary and retry.

### 7.5 PBService Tests

- [ ] Create `pbservice/server_test.go`:
  - [ ] Recreate semantic tests from project:
    - [ ] Basic single primary correctness.
    - [ ] Primary + backup replication.
    - [ ] Fail primary → promote backup.
    - [ ] Backup restart & full snapshot installation.
    - [ ] At-most-once semantics under client retries.
  - [ ] In tests:
    - [ ] Start a gRPC ViewService instance.
    - [ ] Start multiple PBService servers on different ports.
    - [ ] Use `pbservice.Clerk` to run operations and assert state.

---

## 8. Local Run & Dev Workflow

- [ ] Start a viewserver:
  - [ ] `go run ./cmd/viewserver -listen_addr=:8000`
- [ ] Start pbservers:
  - [ ] `go run ./cmd/pbserver -listen_addr=:9001 -view_addr=:8000`
  - [ ] `go run ./cmd/pbserver -listen_addr=:9002 -view_addr=:8000`
- [ ] Use a small demo program:
  - [ ] `cmd/demo/main.go` that:
    - [ ] Creates a `pbservice.Clerk` pointing at viewservice.
    - [ ] Runs some `Put/Get/Append` and prints results.

- [ ] Run tests:
  - [ ] `go test ./...`
  - [ ] For race detection:
    - [ ] `go test ./... -race`

---

## 9. GitHub CI/CD: Auto Testing with GitHub Actions

### 9.1 Basic Go Test Workflow

- [ ] Create `.github/workflows/go.yml`:

```yaml
name: Go CI

on:
  push:
    branches: [main, master]
  pull_request:
    branches: [main, master]

jobs:
  test:
    runs-on: ubuntu-latest

    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: "1.22.x"

      # Optional: cache Go modules
      - name: Cache Go modules
        uses: actions/cache@v4
        with:
          path: |
            ~/go/pkg/mod
            ~/.cache/go-build
          key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}
          restore-keys: |
            ${{ runner.os }}-go-

      # If you DON'T commit generated .pb.go files, generate them here:
      # - name: Install protoc & plugins
      #   run: |
      #     sudo apt-get update && sudo apt-get install -y protobuf-compiler
      #     go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
      #     go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
      #     make proto

      - name: Run tests
        run: go test ./... -v
```

- [ ] Decide on strategy for `.pb.go` files:
  - [ ] Option A (simpler for CI): Commit generated .pb.go files into repo → no protoc step in CI.

  - [ ] Option B: Generate in CI using protoc + make proto.

### 9.2 Optional: Race Detector Job

- [ ] Add second job for race detection:

  ```yaml
    race:
  runs-on: ubuntu-latest
  needs: test

  steps:
   - uses: actions/checkout@v4
   - uses: actions/setup-go@v5
     with:
       go-version: '1.22.x'
   - name: Run tests with -race
     run: go test ./... -race

  ```

---

## 10. Repo Hygiene & Developer Docs

- [ ] Add README.md:
  - [ ] Quick description of gRPC rewrite.

  - [ ] How to install tools.

  - [ ] How to run servers & tests.

- [ ] Add .gitignore:
  - [ ] bin/
  - [ ] .vscode/, .idea/

  - [ ] \*.log

  - [ ] coverage.out

- [ ] Add Makefile conveniences:
  - [ ] make proto → run protoc

  - [ ] make test → go test ./...

  - [ ] make race → go test ./... -race
