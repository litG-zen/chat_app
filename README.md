An Async,Bidirectional Chat Application using gRPC.

How to spin up your own server and chat.

Step 1: Run the Server from one terminal. type go run server/main.go from root directory.

Step 2: Open two terminals

Terminal 1 : go run client/main.go USERID1 localhost:50051 USERID2

Terminal 2 : go run client/main.go USERID2 localhost:50051 USERID1


To become a broadcaster pass the target client userid as *