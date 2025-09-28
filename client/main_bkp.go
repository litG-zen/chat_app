package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"time"

	pb "github.com/litG-zen/chat_app/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func runClient(myID, addr, targetID string) error {
	/*
	 grpc.WithTransportCredentials(insecure.NewCredentials())
	 - This is a DialOption for grpc.Dial()
	 - It tells gRPC what kind of transport security (TLS/SSL) to use on the connection.

	 insecure.NewCredentials()
	  - Creates a set of credentials that do not use TLS (no encryption, no certificate verification).
	*/
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}

	defer conn.Close()
	// Defer works like finally in Python, the function conn.Close(),
	// which closes the stream connection gets called when client ends chat.

	client := pb.NewChatServiceClient(conn)

	if targetID != "*" {
		// First, check if target is online via IsOnline RPC (unary)

		/*
			Go Context : https://www.youtube.com/watch?v=uiUCIz-3CWM
			Docs https://pkg.go.dev/context

			context.Background()

				Creates an empty root context.

				Usually used at the “top” of program when no other context exists.

			context.WithTimeout(...)

				Wraps the parent context (Background here).

				Adds a timeout of 5 seconds.

				After 5 seconds, the new context will be automatically canceled

			How it behaves:

			If the work (e.g., a gRPC call, DB query, etc.) finishes within 5 seconds, nothing special happens.

			If it takes longer than 5 seconds, the context becomes “done” → any operation using it should return with context deadline exceeded.

			we can also call cancel() to stop it earlier.
		*/

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

		defer cancel() //a function call when done early, to free resources.

		resp, err := client.IsOnline(ctx, &pb.IsOnlineRequest{UserId: targetID})
		if err != nil {
			return fmt.Errorf("IsOnline RPC failed: %w", err)
		}
		if !resp.Online {
			fmt.Printf("⚠️ target user %s is offline. You can still chat, messages will deliver when they join.\n", targetID)
		}
		fmt.Printf("target %s is online — opening chat stream...\n", targetID)
	} else {
		fmt.Println("Broadcast mode enabled — messages will be delivered to ALL connected users.")
	} // continue to open Chat stream regardless

	fmt.Printf("target %s is online — opening chat stream...\n", targetID)

	// Open Chat stream
	stream, err := client.Chat(context.Background())
	if err != nil {
		return err
	}

	// send JOIN message to join the server, before communicating with another  party.
	// Unlinke rest, where Json data is passed between sender and receiver; gRPC uses protobuf which is in Binary format, much smaller than JSON/XML.

	// Rest uses serialization/deserialization of data for data transformation.
	// Protobuf uses marshalling/unmarshalling of data for data transformation.
	join := &pb.ChatMessage{ // protobuf payload definition
		UserId:    myID,
		Type:      pb.MessageType_JOIN,
		Timestamp: time.Now().UnixNano() / int64(time.Millisecond),
	}
	if err := stream.Send(join); err != nil {
		return fmt.Errorf("failed to send join: %w", err)
	}

	// StreamMessages continuously listens to incoming messages from the server.
	// This goroutine ensures that the client can receive messages asynchronously
	// while still being able to send messages from stdin.
	go func() {
		for {
			in, err := stream.Recv()
			if err != nil {
				log.Println("stream.Recv error:", err)
				return
			}
			fmt.Printf("\n<< [%s] %s\n> ", in.UserId, in.Text)
		}
	}()

	reader := bufio.NewReader(os.Stdin)
	/*
		bufio.NewReader(os.Stdin)
			What it does:

				os.Stdin is the standard input stream (usually your keyboard input in a terminal).

				bufio.NewReader wraps that input stream with a buffered reader.

				This gives you access to methods like:

				reader.ReadString('\n') → read input until user presses Enter.

				reader.ReadBytes(delim) → read until a delimiter.

				reader.ReadLine() → read one line at a time.

			Why buffer it?

				Reading directly from os.Stdin (via os.Stdin.Read) is low-level and not convenient.

				bufio.Reader adds efficient buffering and utility methods so you don’t have to manually parse bytes.

				Instead of reading one byte at a time from stdin, it grabs chunks into memory and lets you work line by line, string by string.
	*/
	fmt.Println("Type messages and press Enter. Ctrl+C to exit.")
	for {
		fmt.Print("> ")
		line, err := reader.ReadString('\n') // separate message on Enter click and send them away to other party.
		if err != nil {
			log.Println("read error:", err)
			break
		}

		toField := []string{targetID}
		if targetID == "*" {
			toField = []string{"*"}
		}

		msg := &pb.ChatMessage{ // protobuf payload definition
			UserId:    myID,
			To:        toField,
			Type:      pb.MessageType_MESSAGE,
			Text:      line[:len(line)-1],
			Timestamp: time.Now().UnixNano() / int64(time.Millisecond),
		}
		if err := stream.Send(msg); err != nil {
			log.Println("send error:", err)
			break
		}
	}
	return nil
}

func main() {
	if len(os.Args) < 4 {
		fmt.Println("usage: client <myUserID> <serverAddr> <targetUserID>")
		fmt.Println("example: go run client/main.go alice localhost:50051 bob")
		return
	}

	myID := os.Args[1] // SenderID
	addr := os.Args[2] // ServerAddress in IP:Port format

	// If targetID is "*", this client registers as a broadcaster.
	// The server will relay all messages from this client to all connected clients.
	// Otherwise, messages are directed only to the specified targetID.
	targetID := os.Args[3]

	if err := runClient(myID, addr, targetID); err != nil {
		log.Fatalf("client failed: %v", err)
	}
}
