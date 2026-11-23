//go:build sim

package behavior_test

import (
	"io"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jellevandenhooff/gosim"
	"github.com/jellevandenhooff/gosim/gosimruntime"
)

// echoServer listens on addr:1234 and echoes every connection's bytes back.
func echoServer(t *testing.T, addr string) {
	listener, err := net.Listen("tcp", addr+":1234")
	if err != nil {
		t.Fatal(err)
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go func() {
			buffer := make([]byte, 128)
			for {
				n, err := conn.Read(buffer)
				if err != nil {
					return
				}
				if _, err := conn.Write(buffer[:n]); err != nil {
					return
				}
			}
		}()
	}
}

// TestNetTcpPartitionHoldsPackets writes over a connection while its link is
// cut, for less than the connection's timeout. As with TCP, the bytes arrive
// once the link is back, and the connection goes on.
func TestNetTcpPartitionHoldsPackets(t *testing.T) {
	gosim.NewMachine(gosim.MachineConfig{
		Label:    "a",
		Addr:     netip.MustParseAddr(aAddr),
		MainFunc: func() { echoServer(t, aAddr) },
	})
	b := gosim.NewMachine(gosim.MachineConfig{
		Label: "b",
		Addr:  netip.MustParseAddr(bAddr),
		MainFunc: func() {
			time.Sleep(time.Second)
			conn, err := net.Dial("tcp", aAddr+":1234")
			if err != nil {
				t.Fatal(err)
			}

			gosim.SetConnected(aAddr, bAddr, false)
			if _, err := conn.Write([]byte("hello")); err != nil {
				t.Fatal(err)
			}
			time.Sleep(2 * time.Second)
			gosim.SetConnected(aAddr, bAddr, true)

			conn.SetReadDeadline(time.Now().Add(time.Second))
			resp := make([]byte, 5)
			if _, err := io.ReadFull(conn, resp); err != nil {
				t.Fatal(err)
			}
			if string(resp) != "hello" {
				t.Fatal(string(resp))
			}
		},
	})
	b.Wait()
}

// TestNetTcpPartitionDropsGivenUpDial dials over a cut link. The dial times
// out, and once the link is back the listener accepts no connection for it:
// a connection that gave up does not resend.
func TestNetTcpPartitionDropsGivenUpDial(t *testing.T) {
	var accepted atomic.Int32
	gosimruntime.TestRaceToken.Release()
	gosim.NewMachine(gosim.MachineConfig{
		Label: "a",
		Addr:  netip.MustParseAddr(aAddr),
		MainFunc: func() {
			gosimruntime.TestRaceToken.Acquire()
			listener, err := net.Listen("tcp", aAddr+":1234")
			if err != nil {
				t.Fatal(err)
			}
			for {
				if _, err := listener.Accept(); err != nil {
					return
				}
				accepted.Add(1)
			}
		},
	})
	b := gosim.NewMachine(gosim.MachineConfig{
		Label: "b",
		Addr:  netip.MustParseAddr(bAddr),
		MainFunc: func() {
			time.Sleep(time.Second)
			gosim.SetConnected(aAddr, bAddr, false)
			if _, err := net.Dial("tcp", aAddr+":1234"); err == nil {
				t.Fatal("dial over a cut link worked")
			}
			gosim.SetConnected(aAddr, bAddr, true)
			time.Sleep(time.Second)
		},
	})
	b.Wait()
	if n := accepted.Load(); n != 0 {
		t.Fatalf("accepted %d connections from a dial that gave up", n)
	}
}

// TestNetTcpPartitionDropsCrashedSender writes over a cut link, then crashes
// and restarts the writer before the link is back. A crashed machine does not
// resend, so the bytes never arrive, even though a machine is back at the
// writer's address.
func TestNetTcpPartitionDropsCrashedSender(t *testing.T) {
	var received, started atomic.Int32
	gosimruntime.TestRaceToken.Release()
	a := gosim.NewMachine(gosim.MachineConfig{
		Label: "a",
		Addr:  netip.MustParseAddr(aAddr),
		MainFunc: func() {
			gosimruntime.TestRaceToken.Acquire()
			listener, err := net.Listen("tcp", aAddr+":1234")
			if err != nil {
				t.Fatal(err)
			}
			conn, err := listener.Accept()
			if err != nil {
				t.Fatal(err)
			}
			buffer := make([]byte, 128)
			for {
				n, err := conn.Read(buffer)
				received.Add(int32(n))
				if err != nil {
					return
				}
			}
		},
	})
	b := gosim.NewMachine(gosim.MachineConfig{
		Label: "b",
		Addr:  netip.MustParseAddr(bAddr),
		MainFunc: func() {
			gosimruntime.TestRaceToken.Acquire()
			if started.Add(1) > 1 {
				// Restarted: stay up, idle.
				time.Sleep(time.Hour)
			}
			time.Sleep(time.Second)
			conn, err := net.Dial("tcp", aAddr+":1234")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Write([]byte("x")); err != nil {
				t.Fatal(err)
			}
			time.Sleep(100 * time.Millisecond)

			gosim.SetConnected(aAddr, bAddr, false)
			if _, err := conn.Write([]byte("hello")); err != nil {
				t.Fatal(err)
			}
			time.Sleep(time.Hour)
		},
	})

	time.Sleep(2 * time.Second)
	b.Crash()
	gosimruntime.TestRaceToken.Release()
	b.Restart()
	for started.Load() < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	gosim.SetConnected(aAddr, bAddr, true)
	time.Sleep(time.Second)

	if n := received.Load(); n != 1 {
		t.Fatalf("received %d bytes, want only the 1 sent before the link was cut", n)
	}
	a.Crash()
	b.Crash()
}
