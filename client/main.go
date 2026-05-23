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

// resolveAddr accepts many forms and returns an address suitable for grpc.NewClient,
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
		log.Printf("connecting to %s with TLS (serverName=%s)", addr, serverName)
	} else {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		log.Printf("connecting to %s without TLS (insecure)", addr)
	}

	// grpc.NewClient replaces the deprecated grpc.Dial. Connections are lazy by
	// default — they’ll be established on the first RPC (IsOnline / Chat below).
	conn, err := grpc.NewClient(addr, dialOpts...)
	if err != nil {
		return fmt.Errorf("grpc.NewClient(%q): %w", addr, err)
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

	// Subscribe to the target's presence so we get ONLINE/OFFLINE updates on
	// this stream. Skipped in broadcast mode where there is no single target.
	if targetID != "*" {
		sub := &pb.ChatMessage{
			UserId:    myID,
			To:        []string{targetID},
			Type:      pb.MessageType_PRESENCE_SUBSCRIBE,
			Timestamp: time.Now().UnixNano() / int64(time.Millisecond),
		}
		if err := stream.Send(sub); err != nil {
			log.Printf("warning: failed to subscribe to presence for %s: %v", targetID, err)
		}
	}

	go func() {
		for {
			in, err := stream.Recv()
			if err != nil {
				log.Println("stream.Recv error:", err)
				return
			}
			switch in.Type {
			case pb.MessageType_PRESENCE_ONLINE:
				fmt.Printf("\n-- %s is online --\n> ", in.UserId)
			case pb.MessageType_PRESENCE_OFFLINE:
				if in.Text == "subscription_limit" {
					fmt.Printf("\n-- subscription limit reached; cannot watch %s --\n> ", in.UserId)
				} else {
					fmt.Printf("\n-- %s is offline --\n> ", in.UserId)
				}
			case pb.MessageType_TYPING_START:
				fmt.Printf("\n-- %s is typing... --\n> ", in.UserId)
			case pb.MessageType_TYPING_STOP:
				fmt.Printf("\n-- %s stopped typing --\n> ", in.UserId)
			default:
				fmt.Printf("\n<< [%s] %s\n> ", in.UserId, in.Text)
			}
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
	// Typing indicator is a CLI heuristic: we emit TYPING_START right after
	// drawing the prompt (we don't see keystrokes — stdin is line-buffered)
	// and TYPING_STOP just before the MESSAGE goes out. Server-side TTL
	// covers the "user walked away from prompt" case. Broadcast mode skips
	// typing events entirely since there is no single recipient.
	isDM := targetID != "*"
	for {
		if isDM {
			startMsg := &pb.ChatMessage{
				UserId:    myID,
				To:        []string{targetID},
				Type:      pb.MessageType_TYPING_START,
				Timestamp: time.Now().UnixNano() / int64(time.Millisecond),
			}
			if err := stream.Send(startMsg); err != nil {
				log.Println("typing_start send error:", err)
				break
			}
		}

		fmt.Print("> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			log.Println("read error:", err)
			break
		}

		if isDM {
			stopMsg := &pb.ChatMessage{
				UserId:    myID,
				To:        []string{targetID},
				Type:      pb.MessageType_TYPING_STOP,
				Timestamp: time.Now().UnixNano() / int64(time.Millisecond),
			}
			if err := stream.Send(stopMsg); err != nil {
				log.Println("typing_stop send error:", err)
				break
			}
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
