# 💬 Go gRPC ChatApp

A **real-time chat application** built in **Golang** using **gRPC bidirectional streaming** and **Protocol Buffers**.  
Supports **direct messaging** between users and **broadcast messages** to all connected clients.


<img width="2878" height="1700" alt="Screenshot 2025-09-26 210639" src="https://github.com/user-attachments/assets/50238f81-59d8-4fad-8280-f5c2c556f039" />

---

## ✨ Features
- 🚀 **gRPC Bidirectional Streaming** – clients can send and receive messages over a single stream.
- 👤 **Direct Messaging** – chat with a specific user.
- 📢 **Broadcast Mode** – send messages to all connected users with a single `*`.
- 🟢 **Online Status Check** – verify if a user is online via `IsOnline` RPC.
- 🔗 **Concurrent Clients** – multiple clients can connect and chat simultaneously.
- 🛡️ **Extensible Design** – ready for auth, persistence, and scaling with Redis/NATS.
- 📤📥 **Async Commnication**  - communication that happens with independent timing, allowing flexibility and non-blocking interactions.
- 📡  **Domain Name support** - Chat with a friend far far away, using centralised hosted server and its domain name.
---

## 📦 Tech Stack
- **Language**: Golang 1.22+
- **RPC Framework**: gRPC
- **IDL**: Protocol Buffers (`proto3`)
- **Transport**: TCP

---

## **How to spin up your own server and chat.**

**Step 1**: Run the Server from one terminal. type go run server/main.go from root directory.

**Step 2**: Open two terminals

`Terminal 1` : go run client/main.go USERID1 localhost:50051 USERID2

`Terminal 2` : go run client/main.go USERID2 localhost:50051 USERID1


**To become a broadcaster** 
pass the target client userid as *
