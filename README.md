An Async,Bidirectional Chat Application using gRPC.

How to spin up your own server and chat.

Step 1: Run the Server. 
        type go run server/main.go from root directory.

Step 2: Open two terminals
        Terminal 1 : go run client/main.go <user1> localhost:50051 <user2>
        Terminal 2 : go run client/main.go <user2> localhost:50051 <user1>