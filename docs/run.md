# Project 2: Primary/Backup Key/Value Service

---

## Introduction

**This project is modified from Project 2, University of Michigan, EECS491 Distributed System, Spring 2025.**

In Project 1, handling failures was relatively easy because the
Workers did not maintain state. The Manager did maintain state,
but you didn't have to make it fault-tolerant. This project is a
first step towards fault tolerance for servers with state.

## Road map for projects 2-4

In the next 3 projects, you will build several key/value services. The
services support three RPCs: Put(key, value), Append(key, arg), and Get(key).
The service maintains a simple database of key/value pairs. Put() replaces the
value for a particular key in the database. Append(key, arg) appends arg to
key's current value. Get() fetches the current value for a key.

The projects differ in the degree of fault tolerance and performance they
provide for the key/value service:

- Project 2 uses primary/backup replication, assisted by a view
  service that decides which machines are responsible for servicing
  requests. The view service allows the primary/backup service to
  work correctly in the presence of network partitions and node
  failures, as long as the primary and backup do not fail
  simultaneously. The view service itself is not replicated, and is a
  single point of failure.

- Project 3 uses the Paxos distributed consensus protocol to
  replicate the key/value database with no single point of failure,
  and handles network partitions correctly. This key/value service is
  slower than a non-replicated key/value server would be, but provides
  fault tolerance without the arbitrary stalls imposed by
  Primary/Backup.

- Project 4 provides a _sharded_ key/value database. In
  this system, the key/value service can perform Put/Get operations in
  parallel on different shards, allowing it to support applications
  that can put a high load on a storage system. Each shard's state is
  replicated using Paxos, with each shard assigned to a particular
  Paxos group. A replicated configuration service manages these
  assignments and can change the assignment of keys to shards, for
  example, in response to changing load. Project 4 has the core of a
  real-world design for 1000s of servers.

In each project, you will have to do some design. We give you a
sketch of the overall design (and code for the boring pieces), and the
set of RPCs you can use, but you will have to design a complete
protocol given the material from lecture. The tests explore your
protocol's handling of failure scenarios as well as general
correctness. You may need to re-design (and thus re-implement) in
light of problems exposed by the tests; careful thought and planning
may help you avoid too many re-design cycles. We don't give you a
description of the test cases (other than the Go code); in the real
world, you would have to come up with them yourself. Some of the more
advanced test cases are hidden.

## Overview of Project 2

In this project, you'll build a fault-tolerant key/value service
using a form of primary/backup replication. In order to ensure that
all parties (clients and servers) agree on which server is the
primary, and which is the backup, we'll introduce a new server called
the _view service_. The view service monitors whether each
potential server is available or not. If the current primary or backup
is unavailable for "long enough", the view service selects a server to
replace it. A client checks with the view service to find the current
primary. The servers cooperate with the view service to ensure that at
most one primary is active at a time.

**Note**: when we say a server is "unavailable" to the view service,
that means _unavailable from the view service's perspective._
The server may or may not be alive, and if alive the clients and other
servers may or may not still be able to talk with it. This is a major
source of complexity in this project!

Your key/value service will suport replacement of unavailable
servers. If the primary is judged to be unavailable, the view service
will promote the backup to be primary. If the backup is judged to be
unavailable, or is promoted, and there is an idle server available,
the view service will recruit it to be the backup. The newly-promoted
primary will send its complete database to the new backup, and then
send subsequent requests to the backup to ensure that the backup's
key/value database remains identical to the primary's. This also
serves to prevent a "split brain" wherein the primary mistakenly
believes it is still the primary even after it has been replaced.

A key/value server which fails may restart, but it will do so without
a copy of the replicated data (i.e., the keys and values). In our
model, your key/value server will keep the data in memory transiently,
not on disk persistently. One consequence of this model is that if the
primary failes without a fully-instantiated backup in place, it cannot
resume its role as the primary, and the system can no longer serve any
requests.

Likewise, if a key/value server has not failed but learns it has been
declared unavailable by the view service, it must "forget" its current
snapshot of the database. We do this because the server may have
missed some updates during the time it was declared unavailable,
leaving this server's copy out of date. There are ways to minimize the
impact of this (e.g. by versioning plus logging) but they are beyond
the scope of this project.

Only RPCs may be used for interaction between clients and servers,
between different servers, and between different clients. For example,
different instances of your server are not allowed to share Go
variables or files. You may not change any of the RPCs you have been
given, nor should you "re-purpose" any of the RPC arguments.

The design outlined here has some fault-tolerance and performance limitations
which make it too weak for real-world use:

- The view service is a single point of failure, because it is not
  replicated.

- The primary and backup process operations one at a time, limiting
  their performance.

- A recovering server must copy a complete database of key/value
  pairs from the primary, which will be slow, even if the recovering
  server has an almost-up-to-date copy of the data already (e.g., only
  missed a few minutes of updates while its network connection was
  temporarily broken).

- The servers don't store the key/value database on disk, so they
  can't survive simultaneous crashes (e.g., a site-wide power
  failure).

- If a temporary problem prevents primary to backup communication,
  the system has only two remedies: change the view to eliminate the
  backup, or keep trying; neither performs well if such problems are
  frequent.

- If a primary fails before acknowledging the view in which it is
  primary, the view service cannot make progress---it will spin
  forever and not perform a view change.

We will address some of these limitations in subsequent projects by using
better designs and protocols. This project will help you understand the
problems that you'll solve in the succeeding projects.

## Software

Download the archive <proj2.tar.gz> and uncompress
it to get the skeleton code and tests in viewservice and
pbservice.

```bash
% cd proj2/viewservice
% go test
Test: First primary ...
wanted: View{primary: "/var/tmp/824-501/viewserver-68948-1", Backup: "", Viewnum: 1}
got:    View{primary: "", Backup: "", Viewnum: 0}
--- FAIL: Test1 (0.10s)
    viewservice\_test.go:21: wanted primary /var/tmp/824-501/viewserver-68948-1, got
FAIL
exit status 1
FAIL    umich.edu/eecs491/proj2/viewservice     0.349s
```

## Part A: The Viewservice

In Part A you will implement a view service and make sure it passes
our tests; in Part B, you will build the key/value service. Your view
service won't be replicated, so it will be relatively
straightforward. Part B is _much_ more complex than part A,
because the Key/Value service is replicated and you have to design
much of the replication protocol based on lecture material.

The view service goes through a sequence of numbered
_views_, each with a primary and (if possible) a backup. A view
consists of a view number and the identity (network port name) of the
view's primary and backup servers. The view service maintains a
single view--the current view--at all times. It responds with this
view in response to all _Ping_ and
_Get_ requests.

The primary in view _N_ must always be either the primary or
the backup of view _N-1_. This helps ensure that the key/value
service's state is preserved. An exception: when the view service
first starts, it should accept any server at all as the first primary;
at that point, the Key/Value database is empty. The backup in a view
can be any server (other than the primary), or can be altogether
missing if no server is available (represented by an empty
string, "").

Each key/value server should send a Ping RPC once
per PingInterval (see viewservice/common.go). The
view service replies to the Ping with a description of the current
view. A Ping lets the view service know that the key/value
server is alive, informs the view service of the most recent view that
the key/value server knows about, and informs the key/value server of
the current view. If the view service doesn't receive a Ping
from a server for DeadPings PingIntervals, the view
service should consider the server to be unavailable. When a server
restarts after a crash, it should send one or more Pings with a view
number of zero to inform the view service of this fact. This informs
the view server that the restarting server no longer has a current
copy of the Key/Value databse.

The view service attempts to advance to a new view in any of the
following situations:

- Upon startup, when the first available idle server becomes available,
- the current primary or backup is judged to be unavailable,
- the current primary or backup crashed and restarted,
- there is no current backup, and an idle server becomes available

However, the view service **must not** promote the Backup in
view _i_ to be the Primary in view _i+1_ until the primary
from view _i_ acknowledges that it is operating in view _i_
(by sending a Ping where the view number is set to _i_). The
primary should only do so **after** it has successfully trasnferred
its state to the Backup in view _i_.

The acknowledgment rule helps ensure that the view service
advances the view only after the primary has finished bootstrapping
the backup. This prevents the backup from being promoted to become
the new primary before the backup has a copy of the primary's state.
The downside of the acknowledgement rule is that if the primary fails
before it acknowledges the view in which it is primary, then the view
service cannot ever change views again.

Note that you have some flexibility about what to do in some
sitautions, but not in others. If a Primary fails and restarts,
it **cannot** continue serving as the Primary, no matter what else
has (or has not) happened in the system. However, there are times when
it might be prudent to allow a node to continue serving in its current
role even if the view service hasn't heard from it
in DeadPings ticks---for example, if the alternative is to
have the service shut down. Explain any such decisions in your Project
2 Narrative.

We provide you with a complete client.go and appropriate RPC
definitions in common.go. Your job is to supply the needed
code in server_impl.go. When you're done, you should pass
all the tests in viewservice_test.go. To pass additional
tests on the autograder, you are encouraged to test your view service
more extensively. We will neither disclose nor offer hints about the
contents of the hidden test cases.

Things to pay attention to:

- There may be more than two servers sending Pings. The extra ones
  (beyond primary and backup) are volunteering to be backup if
  needed.
- The view service needs a way to detect that a primary or backup
  has failed and restarted. For example, the primary may crash and
  quickly restart without missing sending a single Ping. Hint: this
  is also why views must be numbered.
- You should advance views as soon as possible, but no
  sooner. Advancing a view should be done as completely as
  possible. For example, if you are promoting a Backup to be the new
  Primary, and there is _any_ node available to be recruited
  as the new Backup, the promotion and recruitment should both
  happen in a single step.
- Study the test cases before you start programming. If you fail a
  test, you may have to look at the test code
  in viewservice_test.go to figure out the failure
  scenario.
- Remember that the Go RPC server framework starts a new thread for
  each received RPC request. Thus, if multiple RPCs arrive at the
  same time (from multiple clients), there may be multiple threads
  running concurrently in the server. However, each of your servers
  should only do one thing at a time. Consider using
  the _confinement idiom_ to ensure this happens.

Importantly, you **do not** have to allow concurrent
client requests.

- For full credit, you must ensure that every goroutine your
  viewserver or pbserver launches will terminate if its enclosing
  server dies. The tests kill a server by closing its dead
  channel. We supply a convenience function isdead that you
  may use to check the state of this channel. Consider protecting
  blocking channel operaitons with a read from this channel, and
  guard any blocking I/O (e.g. RPC calls) with the convenience
  function.
- The easiest way to track down bugs is to insert log.Printf()
  statements, collect the output in a file with go test >
  out, and then think about whether the output matches your
  understanding of how your code should behave.

In handling design decisions, it is reasonable to choose simpler
options over more complex ones, even if the simpler option requires
you to maintain some extra state or take a few more steps. For
example, it is perfectly acceptable for your view service to have
state linear in the number of servers that have ever
called Ping

## Part B: The primary/backup key/value service

The primary/backup key/value server source is in pbservice.
We supply you with the client in pbservice/client.go, and
part of the server in pbservice/server.go. Clients use the
service by creating a Clerk object (see client.go) and
calling its methods, which send RPCs to the service. We define the
RPCs of the server in pbservice/rpcs.go. You may not add to
or modify this set.

Your key/value service should continue operating correctly as long as
there has never been a time at which no server was alive and the
primary has never failed before the corresponding backup has been
established. It should also operate correctly with partitions: a
server that suffers temporary network failure without crashing, or can
talk to some computers but not others. If your service is operating
with just one server, it should be able to incorporate a recovered or
idle server (as backup) so that it can then tolerate another server
failure.

Correct operation means that calls to Clerk.Get(k) return the
latest value set by a previous successful call to Clerk.Put(k,v) or
Clerk.Append(k,v), or an empty string if the key has never seen
either. All modifications should provide at-most-once semantics,
i.e., no modification should be applied multiple times to the
replicated key/value database, despite client retries.

You may assume that the view service never halts or crashes, but
cannot assume that requests to and responses from it will never fail.

Your clients and servers may only communicate using RPC, and both
clients and servers must send RPCs with the call() function
in their respective code bases.

It's crucial that at most one primary be active at any given time. You
should have a clear story worked out for why that's the case for your
design. A danger: suppose in some view S1 is the primary; the view
service changes views so that S2 is the primary; but S1 hasn't yet
heard about the new view and thinks it is still primary. Then some
clients might talk to S1, and others talk to S2, and not see each
others' Put()s. Your solution should account for this by
confirming **every** operation at the backup.

A server that recieves a client request while it is not the active
primary should signal this by setting the Err field of
the OpReply struct to ErrWrongServer.

Clerk.Get(), Clerk.Put(), and Clerk.Append() will only return when
they have completed the operation. The client code we provide ensures
that operations are retried when they fail. This is done under the
assumption that failures are transient and/or can be rectified by an
advance in the view. Your server must account for the duplicate RPCs
that these client retries will generate to ensure at-most-once
semantics for modifications. You may assume that each clerk has only
one outstanding request (Get, Put, or Append). Think carefully about
what the _commit point_ is for a Put or Append; you will
describe this as part of your project narrative. You will also need to
describe how your design chooses the commit point for a Get.

A server should not talk to the view service for every Put/Get
it receives, since that would put the view service on the critical path
for performance and fault-tolerance. Instead servers should
Ping the view service periodically
(in pbservice/server_impl.go's tick())
to learn about new views. Similarly, the client Clerk will not
talk to the view service for every RPC it sends; instead, the
Clerk caches the current primary, and only talks to the
view service when the current view is or seems to be incorrect.

You'll need to ensure that the backup sees every update to the
key/value database, by a combination of the primary initializing it
with the complete key/value database and forwarding subsequent client
operations. Your primary should forward just the arguments to
each Append() to the backup; do not forward the resulting
value, which might be large.

The skeleton code for the key/value servers is in pbservice.
Since the go.mod file included in the skeleton code specifies
the module path as umich.edu/eecs491/proj2, you'll need to
import umich.edu/eecs491/proj2/viewservice to use your
implementation of viewservice in pbservice. See
pbservice/server.go for how this is done.

You can run the tests provided to you as follows:

```bash
% cd proj2/pbservice
% go test
Test: Single primary, no backup ...
--- FAIL: TestBasicFail (2.01s)
    pbservice\_test.go:56: first primary never formed view
...
%
```

Here's a recommended plan of attack:

1.  You should start by modifying pbservice/server_impl.go
    to Ping the view service to find the current view. Do this
    in the tick() function. Once a server knows the
    current view, it knows if it is the primary, the backup,
    or neither.

- Implement code for Get, Put, and Append
  in pbservice/server_impl.go; store keys and values in
  a map[string]string. If a key does not exist, Append should
  use an empty string for the previous value.

- Handle the case when a client re-tries a modification that has already
  been successfully applied.

- Modify your handlers so that the primary forwards updates to the
  backup. Your design has some flexibilty in the order in which this
  happens relative to the local update. You also must decide whether to
  retry failures to the backup or reflect such failures back to the
  client. Note that these decisions are not entirely independent of one
  another. Have a rationale for why you make the decisions you made, and
  be prepared to explain them in your narrative.

- When a server becomes the backup in a new view, the primary should
  send it the primary's complete key/value database, plus any
  information necessary to handle clients re-trying modifications that
  have already been applied, but for which the client did not recieve
  acknowledgement.

You are done if you can pass all the tests
in pbservice_test.go and a few additional tests on the
autograder. You'll see some lots of "rpc: client protocol error" and
"rpc: writing response" and "unexpected EOF" complaints; ignore
them. These are a byproduct of the way our infrastructure simulates
network failures.

Things to pay attention to:

- Unlike Project 1, in which the workers were stateless, when an RPC to
  a server fails in this project, that server may have updated its state but
  its reply may have failed to reach the sender of the RPC.

- Whenever an RPC fails, sleep for viewservice.PingInterval before
  retrying the RPC. This avoids burning too much CPU.

- For full credit, you must ensure that every goroutine your
  viewserver or pbserver launches will terminate if its enclosing
  server dies. The tests kill a server by closing its dead
  channel. We supply a convenience function isdead that you
  may use to check the state of this channel. Consider protecting
  blocking channel operaitons with a read from this channel, and
  guard any blocking I/O (e.g. RPC calls) with the convenience
  function.
- Even if your viewserver passed all the tests in Part A, it
  may still have bugs that cause failures in Part B.

- Study the test cases before you start programming.

## Part C: Documentation and Narratives

As part of this project, you will be given a
repository uniqname.2doc. This repository should contain two
files. The file design.txt has questions about your
viewservice design, and your pbservice design. Answer each of those
questions clearly and concisely.

The file p1comments.txt is an opportunity for you to comment
on other students' solutions to Project 1. Please fill it out for each
of the P1 solutions you are given. Only students who acheive mastery
for Project 1 by the nominal deadline will be given able to complete
this.

The templates for both of these files is in the handout tarball.

## Handin procedure

Clone the two repositories that we have created for you on
GitHub: uniqname.2 and uniqname.2doc

When you submit your project to the [autograder](https://lamport.eecs.umich.edu/eecs491/submit.php?2), it will pull the following files from your repository:

- viewservice/server_impl.go\* pbservice/server_impl.go

So, please ensure that a) your repository has directories
called viewservice and pbservice containing these
files, and b) all modifications that you make to the code handed out
are restricted to only these files.

We will grade your "best" submission: the highest scoring one. In the
event of a tie, we will use the latest submission with that high
score. Your narrative should refer to that version of your submission.

Among the unit tests included in the handout, if you find that you
pass some of them locally on your computer but not on the autograder

- Check that you have only modified the \*impl.go files
- Check that you pass on CAEN all the tests that you pass locally
- Ensure that you use appropriate synchronization to protect any access to shared data from any goroutine; to check, run "go test" on CAEN with the "-race" flag
- Since some of the tests are non-deterministic, run them repeatedly and make sure you pass them every time
- If you still pass all runs of the test cases, post privately on Piazza
