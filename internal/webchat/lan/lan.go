package lan

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

const (
	// broadcastPort is the UDP port used for LAN discovery announcements.
	broadcastPort = 5353
	// announceInterval is how often we broadcast our presence.
	announceInterval = 10 * time.Second
	// listenTimeout is how long Peers() listens for broadcasts.
	listenTimeout = 3 * time.Second
)

// Peer is a discovered agent on the local network.
type Peer struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Port    int    `json:"port"`
}

// announcement is the JSON payload broadcast over UDP.
type announcement struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}

// Discovery provides LAN-based agent discovery via UDP broadcast.
type Discovery struct {
	port      int
	agentName string

	mu      sync.Mutex
	stopCh  chan struct{}
	running bool
}

// New creates a LAN discovery instance.
func New(port int, agentName string) *Discovery {
	return &Discovery{
		port:      port,
		agentName: agentName,
	}
}

// Start begins broadcasting presence via UDP on the broadcast port.
func (d *Discovery) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return fmt.Errorf("lan: already started")
	}
	d.stopCh = make(chan struct{})
	d.running = true

	go d.broadcastLoop(ctx)
	return nil
}

// Stop halts the broadcast loop.
func (d *Discovery) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return nil
	}
	close(d.stopCh)
	d.running = false
	return nil
}

// Peers listens for other agents broadcasting on the LAN.
// It collects announcements for up to listenTimeout and returns unique peers.
func (d *Discovery) Peers(ctx context.Context) ([]Peer, error) {
	addr := &net.UDPAddr{Port: broadcastPort, IP: net.IPv4zero}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, fmt.Errorf("lan: listen: %w", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(listenTimeout))

	seen := make(map[string]Peer)
	buf := make([]byte, 1024)

	for {
		select {
		case <-ctx.Done():
			return peersFromMap(seen), nil
		default:
		}

		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			// Timeout or error — return what we have.
			break
		}

		var ann announcement
		if err := json.Unmarshal(buf[:n], &ann); err != nil {
			continue
		}

		// Skip our own announcements.
		if ann.Name == d.agentName && ann.Port == d.port {
			continue
		}

		key := fmt.Sprintf("%s:%d", remoteAddr.IP, ann.Port)
		seen[key] = Peer{
			Name:    ann.Name,
			Address: remoteAddr.IP.String(),
			Port:    ann.Port,
		}
	}

	return peersFromMap(seen), nil
}

func (d *Discovery) broadcastLoop(ctx context.Context) {
	ann := announcement{Name: d.agentName, Port: d.port}
	data, _ := json.Marshal(ann)

	broadcastAddr := &net.UDPAddr{
		IP:   net.IPv4bcast,
		Port: broadcastPort,
	}

	ticker := time.NewTicker(announceInterval)
	defer ticker.Stop()

	// Broadcast immediately on start.
	d.sendBroadcast(broadcastAddr, data)

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.sendBroadcast(broadcastAddr, data)
		}
	}
}

func (d *Discovery) sendBroadcast(addr *net.UDPAddr, data []byte) {
	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		log.Printf("lan: broadcast dial: %v", err)
		return
	}
	defer conn.Close()
	conn.Write(data)
}

func peersFromMap(m map[string]Peer) []Peer {
	peers := make([]Peer, 0, len(m))
	for _, p := range m {
		peers = append(peers, p)
	}
	return peers
}
