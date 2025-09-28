package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	pb "github.com/litG-zen/chat_app/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// default gRPC port used when user doesn't provide one
const defaultPort = "50051"

// resolveAddr accepts many forms and returns an address suitable for grpc.Dial,
// a serverName for TLS verification (if any), and whether to use TLS.
// Examples accepted:
//
//	"example.com"
//	"example.com:50051"
//	"https://example.com"    -> useTLS=true
//	"http://1.2.3.4:50051"   -> useTLS=false
//	"1.2.3.4"
func resolveAddr(input string) (addr string, serverName string, useTLS bool, err error) {
	// Trim spaces
	input = strings.TrimSpace(input)
	// if input looks like a URL (has scheme)
	if strings.Contains(input, "://") {
		print("Domain name passed")
		u, perr := url.Parse(input)
		if perr != nil {
			return "", "", false, perr
		}
		// some users might pass "https://example.com" (Host will be example.com)
		host := u.Host
		if host == "" {
			// sometimes the host can end up in Path for malformed input
			host = u.Path
		}
		// if host has no port, add default
		if _, _, splitErr := net.SplitHostPort(host); splitErr != nil {
			host = net.JoinHostPort(host, defaultPort)
		}
		// serverName should be hostname (no port)
		serverName = u.Hostname()
		useTLS = strings.EqualFold(u.Scheme, "https")
		return host, serverName, useTLS, nil
	}

	// Not a URL - try host:port split
	host, port, splitErr := net.SplitHostPort(input)
	if splitErr != nil {
		// no port present: treat entire input as host and append default port
		host = input
		port = defaultPort
	}
	addr = net.JoinHostPort(host, port)

	// If host is an IP literal, net.ParseIP returns non-nil
	if ip := net.ParseIP(host); ip != nil {
		// IP literal -> default to insecure unless user used URL scheme previously
		useTLS = false
		serverName = "" // no SNI verification by default
		return addr, serverName, useTLS, nil
	}

	// Otherwise host is host:port -> use TLS=false and use host:port as server name
	useTLS = false
	serverName = host
	return addr, serverName, useTLS, nil
}

func runClient(myID, rawAddr, targetID string) error {
	addr, serverName, useTLS, err := resolveAddr(rawAddr)
	if err != nil {
		return fmt.Errorf("invalid server address %q: %w", rawAddr, err)
	}

	var dialOpts []grpc.DialOption

	if useTLS {
		// Use system root CAs and verify serverName (SNI)
		creds := credentials.NewClientTLSFromCert(nil, serverName)
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(creds))
		log.Printf("Dialing %s with TLS (serverName=%s)", addr, serverName)
	} else {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		log.Printf("Dialing %s without TLS (insecure)", addr)
	}

	conn, err := grpc.Dial(addr, dialOpts...)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pb.NewChatServiceClient(conn)

	if targetID != "*" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
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
	}

	// Open Chat stream
	stream, err := client.Chat(context.Background())
	if err != nil {
		return err
	}

	join := &pb.ChatMessage{
		UserId:    myID,
		Type:      pb.MessageType_JOIN,
		Timestamp: time.Now().UnixNano() / int64(time.Millisecond),
	}
	if err := stream.Send(join); err != nil {
		return fmt.Errorf("failed to send join: %w", err)
	}

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
	fmt.Println("Type messages and press Enter. Ctrl+C to exit.")
	for {
		fmt.Print("> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			log.Println("read error:", err)
			break
		}

		toField := []string{targetID}
		if targetID == "*" {
			toField = []string{"*"}
		}

		msg := &pb.ChatMessage{
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
		fmt.Println("examples:")
		fmt.Println("  go run client/main.go alice example.com bob")
		fmt.Println("  go run client/main.go alice example.com:50051 bob")
		fmt.Println("  go run client/main.go alice https://example.com bob")
		fmt.Println("  go run client/main.go alice 1.2.3.4:50051 bob")
		return
	}

	myID := os.Args[1]
	addr := os.Args[2]
	targetID := os.Args[3]

	if err := runClient(myID, addr, targetID); err != nil {
		log.Fatalf("client failed: %v", err)
	}
}
