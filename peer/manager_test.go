package peer

import (
	"net"
	"testing"
	"time"

	"github.com/babafeng/pasted/network"
)

func TestBroadcastExceptSkipsSourceConnectionKey(t *testing.T) {
	mgr := NewManager()

	sourceLocal, sourceRemote := net.Pipe()
	targetLocal, targetRemote := net.Pipe()
	defer sourceLocal.Close()
	defer sourceRemote.Close()
	defer targetLocal.Close()
	defer targetRemote.Close()

	mgr.Add("10.0.0.1:1001", &Peer{
		Hostname: "client-a",
		IP:       "10.0.0.1",
		Client:   network.NewClientFromConn(sourceLocal),
	})
	mgr.Add("10.0.0.1:1002", &Peer{
		Hostname: "client-b",
		IP:       "10.0.0.1",
		Client:   network.NewClientFromConn(targetLocal),
	})

	frame := network.NewFrame(network.TypeText, []byte("hello"))
	received := make(chan *network.Frame, 1)
	errCh := make(chan error, 1)
	go func() {
		got, err := network.DecodeFrame(targetRemote)
		if err != nil {
			errCh <- err
			return
		}
		received <- got
	}()

	sent := mgr.BroadcastExcept("10.0.0.1:1001", &frame)
	if len(sent) != 1 || sent[0].Hostname != "client-b" {
		t.Fatalf("expected only client-b to receive frame, got %#v", sent)
	}

	select {
	case got := <-received:
		if string(got.Payload) != "hello" {
			t.Fatalf("unexpected payload: %q", got.Payload)
		}
	case err := <-errCh:
		t.Fatalf("decode failed: %v", err)
	case <-time.After(time.Second):
		t.Fatal("target client did not receive frame")
	}

	_ = sourceRemote.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	buf := make([]byte, 1)
	if _, err := sourceRemote.Read(buf); err == nil {
		t.Fatal("source client should not receive its own frame")
	}
}

func TestBroadcastRemovesFailedPeerByConnectionKey(t *testing.T) {
	mgr := NewManager()

	brokenLocal, brokenRemote := net.Pipe()
	targetLocal, targetRemote := net.Pipe()
	defer brokenRemote.Close()
	defer targetLocal.Close()
	defer targetRemote.Close()

	brokenClient := network.NewClientFromConn(brokenLocal)
	mgr.Add("10.0.0.1:1001", &Peer{
		Hostname: "broken",
		IP:       "10.0.0.1",
		Client:   brokenClient,
	})
	mgr.Add("10.0.0.1:1002", &Peer{
		Hostname: "healthy",
		IP:       "10.0.0.1",
		Client:   network.NewClientFromConn(targetLocal),
	})

	if err := brokenClient.Close(); err != nil {
		t.Fatalf("close broken client: %v", err)
	}

	frame := network.NewFrame(network.TypeText, []byte("hello"))
	received := make(chan error, 1)
	go func() {
		_, err := network.DecodeFrame(targetRemote)
		received <- err
	}()

	sent := mgr.Broadcast(&frame)
	if len(sent) != 1 || sent[0].Hostname != "healthy" {
		t.Fatalf("expected only healthy peer to receive frame, got %#v", sent)
	}

	select {
	case err := <-received:
		if err != nil {
			t.Fatalf("healthy peer decode failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("healthy peer did not receive frame")
	}

	if mgr.Has("10.0.0.1:1001") {
		t.Fatal("failed peer was not removed by its connection key")
	}
	if !mgr.Has("10.0.0.1:1002") {
		t.Fatal("healthy peer with same IP was removed")
	}
}
