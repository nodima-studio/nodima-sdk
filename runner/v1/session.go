package v1

import (
	"errors"
	"fmt"
)

// Session messages frame one or more remote nodes over an SSH control
// transport. The host completes topology and peer setup before session_run is
// allowed to start any runner.
const (
	MessageSessionStart     MessageType = "session_start"
	MessageSessionReady     MessageType = "session_ready"
	MessageSessionConnect   MessageType = "session_connect"
	MessageSessionConnected MessageType = "session_connected"
	MessageSessionRun       MessageType = "session_run"
	MessageSessionCompleted MessageType = "session_completed"
	MessageEdgeStats        MessageType = "edge_stats"
	MessageCancel           MessageType = "cancel"

	// MessageCredit applies explicit per-edge flow control on peer transports.
	MessageCredit MessageType = "credit"
)

const (
	MaxSessionNodes = 256
	MaxSessionEdges = 4_096
)

type SessionStart struct {
	ExecutionID string        `cbor:"execution_id"`
	HostID      string        `cbor:"host_id,omitempty"`
	Nodes       []SessionNode `cbor:"nodes"`
	// Edges names the links both of whose endpoints run in this session.
	Edges        []SessionEdge     `cbor:"edges,omitempty"`
	Boundaries   []SessionBoundary `cbor:"boundaries,omitempty"`
	PeerEdges    []SessionPeerEdge `cbor:"peer_edges,omitempty"`
	TargetNodeID string            `cbor:"target_node_id,omitempty"`
	PeerTLS      *SessionTLS       `cbor:"peer_tls,omitempty"`
	// WallTimeMS bounds the whole session. The agent enforces it itself so a
	// lost connection cannot leave work running on someone else's machine.
	WallTimeMS uint64 `cbor:"wall_time_ms,omitempty"`
}

type SessionNode struct {
	NodeID        string            `cbor:"node_id"`
	PackageID     string            `cbor:"package_id"`
	Version       string            `cbor:"version"`
	Config        map[string]string `cbor:"config,omitempty"`
	InputPorts    []string          `cbor:"input_ports,omitempty"`
	OutputPorts   []string          `cbor:"output_ports,omitempty"`
	Capabilities  []Capability      `cbor:"capabilities,omitempty"`
	PackageDigest string            `cbor:"package_digest,omitempty"`
	RunnerGrant   *RunnerGrant      `cbor:"runner_grant,omitempty"`
	SecretValues  map[string]string `cbor:"secret_values,omitempty"`
	// SpillLimits are effective host limits for trusted spilling built-ins.
	// Installed packages continue to use RunnerGrant.Scratch instead.
	SpillLimits    *SpillLimits `cbor:"spill_limits,omitempty"`
	ReplicaHostIDs []string     `cbor:"replica_host_ids,omitempty"`
	ReplicaWorker  bool         `cbor:"replica_worker,omitempty"`
}

// RunnerGrant is the transport form of an execution plan's explicit
// capability grant. JSON project contracts may alias these types so the public
// runner ABI does not depend on an application's persisted project schema.
type RunnerGrant struct {
	CompositeDefinitionID string           `json:"compositeDefinitionId,omitempty" cbor:"composite_definition_id,omitempty"`
	NodeID                string           `json:"nodeId" cbor:"node_id"`
	HTTP                  []HTTPGrant      `json:"http,omitempty" cbor:"http,omitempty"`
	FileRead              []FileReadGrant  `json:"fileRead,omitempty" cbor:"file_read,omitempty"`
	FileWrite             []FileWriteGrant `json:"fileWrite,omitempty" cbor:"file_write,omitempty"`
	Scratch               *ScratchGrant    `json:"scratch,omitempty" cbor:"scratch,omitempty"`
	Secrets               []string         `json:"secrets,omitempty" cbor:"secrets,omitempty"`
}

type HTTPGrant struct {
	Scheme string   `json:"scheme" cbor:"scheme"`
	Host   string   `json:"host" cbor:"host"`
	Ports  []uint16 `json:"ports" cbor:"ports"`
}
type FileReadGrant struct {
	Name     string `json:"name" cbor:"name"`
	Root     string `json:"root" cbor:"root"`
	MaxBytes uint64 `json:"maxBytes" cbor:"max_bytes"`
}
type FileWriteGrant struct {
	Name     string `json:"name" cbor:"name"`
	Root     string `json:"root,omitempty" cbor:"root,omitempty"`
	MaxBytes uint64 `json:"maxBytes" cbor:"max_bytes"`
	Delivery string `json:"delivery" cbor:"delivery"`
}
type ScratchGrant struct {
	MaxBytes uint64 `json:"maxBytes" cbor:"max_bytes"`
	MaxFiles uint32 `json:"maxFiles" cbor:"max_files"`
}

type SpillLimits struct {
	MemoryBytes  uint64 `cbor:"memory_bytes"`
	ScratchBytes uint64 `cbor:"scratch_bytes"`
	ScratchFiles uint32 `cbor:"scratch_files"`
	FrameBytes   uint32 `cbor:"frame_bytes"`
	MergeFanIn   uint32 `cbor:"merge_fan_in"`
}

func DefaultSpillLimits() SpillLimits {
	return SpillLimits{MemoryBytes: 32 << 20, ScratchBytes: 2 << 30, ScratchFiles: 128, FrameBytes: 1 << 20, MergeFanIn: 8}
}

func (l SpillLimits) WithDefaults() SpillLimits {
	d := DefaultSpillLimits()
	if l.MemoryBytes == 0 {
		l.MemoryBytes = d.MemoryBytes
	}
	if l.ScratchBytes == 0 {
		l.ScratchBytes = d.ScratchBytes
	}
	if l.ScratchFiles == 0 {
		l.ScratchFiles = d.ScratchFiles
	}
	if l.FrameBytes == 0 {
		l.FrameBytes = d.FrameBytes
	}
	if l.MergeFanIn == 0 {
		l.MergeFanIn = d.MergeFanIn
	}
	return l
}

func (l SpillLimits) Validate() error {
	l = l.WithDefaults()
	if l.ScratchFiles < 4 {
		return errors.New("spilling requires at least four scratch files")
	}
	if l.MemoryBytes == 0 || l.ScratchBytes == 0 || l.FrameBytes == 0 {
		return errors.New("spilling limits must be positive")
	}
	if l.MergeFanIn < 2 || l.MergeFanIn > 8 {
		return errors.New("spill merge fan-in must be between 2 and 8")
	}
	return nil
}

type SessionEdge struct {
	EdgeID     string `cbor:"edge_id"`
	FromNodeID string `cbor:"from_node_id"`
	FromPortID string `cbor:"from_port_id"`
	ToNodeID   string `cbor:"to_node_id"`
	ToPortID   string `cbor:"to_port_id"`
}

type SessionBoundary struct {
	EdgeID    string `cbor:"edge_id"`
	NodeID    string `cbor:"node_id"`
	PortID    string `cbor:"port_id"`
	Direction string `cbor:"direction"`
}

const (
	BoundaryInput  = "input"
	BoundaryOutput = "output"
)

// SessionTLS is short-lived peer material delivered only inside the SSH
// session. It is never persisted by either side.
type SessionTLS struct {
	CA          []byte `cbor:"ca"`
	Certificate []byte `cbor:"certificate"`
	PrivateKey  []byte `cbor:"private_key"`
}

type SessionConnect struct {
	HostID string            `cbor:"host_id"`
	Peers  []SessionPeer     `cbor:"peers,omitempty"`
	Edges  []SessionPeerEdge `cbor:"edges,omitempty"`
}

type SessionPeer struct {
	HostID  string `cbor:"host_id"`
	Address string `cbor:"address"`
	Port    uint16 `cbor:"port"`
}

type SessionPeerEdge struct {
	EdgeID     string `cbor:"edge_id"`
	FromHostID string `cbor:"from_host_id"`
	FromNodeID string `cbor:"from_node_id"`
	FromPortID string `cbor:"from_port_id"`
	ToHostID   string `cbor:"to_host_id"`
	ToNodeID   string `cbor:"to_node_id"`
	ToPortID   string `cbor:"to_port_id"`
}

// SessionReady is the agent's answer to session_start. The host compares SHA256
// against the binary it installed before letting any data move, which is what
// catches a stale agent left at a path from an earlier build.
type SessionReady struct {
	ABI          string `cbor:"abi"`
	AgentVersion string `cbor:"agent_version,omitempty"`
	SHA256       string `cbor:"sha256,omitempty"`
	OS           string `cbor:"os,omitempty"`
	Arch         string `cbor:"arch,omitempty"`
	PeerPort     uint16 `cbor:"peer_port,omitempty"`
}

type Credit struct {
	EdgeID  string `cbor:"edge_id,omitempty"`
	NodeID  string `cbor:"node_id,omitempty"`
	PortID  string `cbor:"port_id"`
	Batches uint32 `cbor:"batches"`
}

type EdgeStats struct {
	EdgeID  string `cbor:"edge_id"`
	Batches uint64 `cbor:"batches"`
	Rows    uint64 `cbor:"rows"`
	Bytes   uint64 `cbor:"bytes"`
}

type SessionCompleted struct {
	Preview *SessionPreview `cbor:"preview,omitempty"`
}

type SessionPreview struct {
	Ports []SessionPreviewPort `cbor:"ports"`
}

type SessionPreviewPort struct {
	PortID       string                 `cbor:"port_id"`
	Columns      []SessionPreviewColumn `cbor:"columns"`
	Rows         [][]*string            `cbor:"rows"`
	RowsProduced uint64                 `cbor:"rows_produced"`
	Truncated    bool                   `cbor:"truncated"`
}

type SessionPreviewColumn struct {
	Name     string `cbor:"name"`
	Type     string `cbor:"type"`
	Nullable bool   `cbor:"nullable"`
}

func (p SessionPreview) Validate() error {
	if len(p.Ports) > MaxSessionNodes {
		return errors.New("session preview has too many ports")
	}
	rows, bytes := 0, 0
	ports := map[string]struct{}{}
	for _, port := range p.Ports {
		if port.PortID == "" {
			return errors.New("session preview port requires an ID")
		}
		if _, duplicate := ports[port.PortID]; duplicate {
			return fmt.Errorf("session preview repeats port %q", port.PortID)
		}
		ports[port.PortID] = struct{}{}
		if len(port.Columns) > 4_096 {
			return fmt.Errorf("session preview port %q has too many columns", port.PortID)
		}
		rows += len(port.Rows)
		for _, row := range port.Rows {
			if len(row) != len(port.Columns) {
				return fmt.Errorf("session preview port %q row width does not match its columns", port.PortID)
			}
			for _, value := range row {
				if value != nil {
					bytes += len(*value)
				}
			}
		}
	}
	if rows > 1_000 || bytes > 1<<20 {
		return errors.New("session preview exceeds its bounded snapshot limit")
	}
	return nil
}

func (s SessionStart) Validate() error {
	if s.ExecutionID == "" {
		return errors.New("session_start requires an execution ID")
	}
	if len(s.Nodes) == 0 {
		return errors.New("session_start requires at least one node")
	}
	if len(s.Nodes) > MaxSessionNodes {
		return fmt.Errorf("session_start exceeds %d nodes", MaxSessionNodes)
	}
	if len(s.Edges) > MaxSessionEdges {
		return fmt.Errorf("session_start exceeds %d edges", MaxSessionEdges)
	}
	if len(s.Boundaries) > MaxSessionEdges {
		return fmt.Errorf("session_start exceeds %d boundaries", MaxSessionEdges)
	}
	if len(s.PeerEdges) > MaxSessionEdges {
		return fmt.Errorf("session_start exceeds %d peer edges", MaxSessionEdges)
	}
	if len(s.PeerEdges) > 0 && (s.HostID == "" || s.PeerTLS == nil) {
		return errors.New("session_start peer edges require a host ID and TLS material")
	}
	if s.PeerTLS != nil {
		if len(s.PeerTLS.CA) == 0 || len(s.PeerTLS.Certificate) == 0 || len(s.PeerTLS.PrivateKey) == 0 {
			return errors.New("session_start peer TLS requires CA, certificate, and private key")
		}
		if len(s.PeerTLS.CA)+len(s.PeerTLS.Certificate)+len(s.PeerTLS.PrivateKey) > 64<<10 {
			return errors.New("session_start peer TLS material exceeds its byte limit")
		}
	}

	nodes := make(map[string]struct{}, len(s.Nodes))
	for _, node := range s.Nodes {
		if node.NodeID == "" {
			return errors.New("session_start node requires an ID")
		}
		if _, exists := nodes[node.NodeID]; exists {
			return fmt.Errorf("session_start repeats node %q", node.NodeID)
		}
		nodes[node.NodeID] = struct{}{}
		if node.PackageID == "" || node.Version == "" {
			return fmt.Errorf("session_start node %q requires a package and version", node.NodeID)
		}
		if node.PackageDigest != "" {
			if len(node.PackageDigest) != 64 {
				return fmt.Errorf("session_start node %q has an invalid package digest", node.NodeID)
			}
			if len(node.Capabilities) > 0 && node.RunnerGrant == nil {
				return fmt.Errorf("session_start node %q requires a runner grant", node.NodeID)
			}
		}
		if len(node.SecretValues) > 256 {
			return fmt.Errorf("session_start node %q has too many secret values", node.NodeID)
		}
		allowedSecrets := map[string]struct{}{}
		if node.RunnerGrant != nil {
			for _, name := range node.RunnerGrant.Secrets {
				allowedSecrets[name] = struct{}{}
			}
		}
		secretBytes := 0
		for name, value := range node.SecretValues {
			if _, allowed := allowedSecrets[name]; !allowed {
				return fmt.Errorf("session_start node %q includes an ungranted secret", node.NodeID)
			}
			secretBytes += len(name) + len(value)
		}
		if secretBytes > 1<<20 {
			return fmt.Errorf("session_start node %q secret values exceed the byte limit", node.NodeID)
		}
		for _, capability := range node.Capabilities {
			if err := capability.Validate(); err != nil {
				return fmt.Errorf("session_start node %q: %w", node.NodeID, err)
			}
		}
		if node.SpillLimits != nil {
			if node.PackageDigest != "" {
				return fmt.Errorf("session_start node %q gives trusted spill limits to an installed package", node.NodeID)
			}
			if err := node.SpillLimits.Validate(); err != nil {
				return fmt.Errorf("session_start node %q: %w", node.NodeID, err)
			}
		}
	}
	if s.TargetNodeID != "" {
		if _, known := nodes[s.TargetNodeID]; !known {
			return fmt.Errorf("session_start preview target %q is not a session node", s.TargetNodeID)
		}
	}

	edges := make(map[string]struct{}, len(s.Edges)+len(s.Boundaries)+len(s.PeerEdges))
	for _, edge := range s.Edges {
		if edge.EdgeID == "" {
			return errors.New("session_start edge requires an ID")
		}
		if _, exists := edges[edge.EdgeID]; exists {
			return fmt.Errorf("session_start repeats edge %q", edge.EdgeID)
		}
		edges[edge.EdgeID] = struct{}{}
		if _, known := nodes[edge.FromNodeID]; !known {
			return fmt.Errorf("session_start edge %q leaves unknown node %q", edge.EdgeID, edge.FromNodeID)
		}
		if _, known := nodes[edge.ToNodeID]; !known {
			return fmt.Errorf("session_start edge %q enters unknown node %q", edge.EdgeID, edge.ToNodeID)
		}
		if edge.FromPortID == "" || edge.ToPortID == "" {
			return fmt.Errorf("session_start edge %q requires both port IDs", edge.EdgeID)
		}
	}
	for _, boundary := range s.Boundaries {
		if boundary.EdgeID == "" || boundary.NodeID == "" || boundary.PortID == "" {
			return errors.New("session_start boundary requires edge, node, and port IDs")
		}
		if _, known := nodes[boundary.NodeID]; !known {
			return fmt.Errorf("session_start boundary %q references unknown node %q", boundary.EdgeID, boundary.NodeID)
		}
		if _, duplicate := edges[boundary.EdgeID]; duplicate {
			return fmt.Errorf("session_start repeats edge %q", boundary.EdgeID)
		}
		edges[boundary.EdgeID] = struct{}{}
		if boundary.Direction != BoundaryInput && boundary.Direction != BoundaryOutput {
			return fmt.Errorf("session_start boundary %q has invalid direction %q", boundary.EdgeID, boundary.Direction)
		}
	}
	for _, edge := range s.PeerEdges {
		if edge.EdgeID == "" || edge.FromHostID == "" || edge.ToHostID == "" || edge.FromNodeID == "" || edge.ToNodeID == "" || edge.FromPortID == "" || edge.ToPortID == "" {
			return errors.New("session_start peer edge is incomplete")
		}
		_, fromLocal := nodes[edge.FromNodeID]
		_, toLocal := nodes[edge.ToNodeID]
		if fromLocal == toLocal {
			return fmt.Errorf("session_start peer edge %q must have exactly one local endpoint", edge.EdgeID)
		}
		if _, duplicate := edges[edge.EdgeID]; duplicate {
			return fmt.Errorf("session_start repeats edge %q", edge.EdgeID)
		}
		edges[edge.EdgeID] = struct{}{}
		if edge.FromHostID == edge.ToHostID {
			return fmt.Errorf("session_start peer edge %q must cross hosts", edge.EdgeID)
		}
		if (fromLocal && edge.FromHostID != s.HostID) || (toLocal && edge.ToHostID != s.HostID) {
			return fmt.Errorf("session_start peer edge %q has the wrong local host", edge.EdgeID)
		}
	}
	return nil
}

func (s SessionConnect) Validate() error {
	if s.HostID == "" {
		return errors.New("session_connect requires a host ID")
	}
	if len(s.Peers) > MaxSessionNodes || len(s.Edges) > MaxSessionEdges {
		return errors.New("session_connect topology exceeds its limit")
	}
	peers := map[string]struct{}{}
	for _, peer := range s.Peers {
		if peer.HostID == "" || peer.Address == "" || peer.Port == 0 {
			return errors.New("session_connect peer requires host, address, and port")
		}
		if _, duplicate := peers[peer.HostID]; duplicate {
			return fmt.Errorf("session_connect repeats peer %q", peer.HostID)
		}
		peers[peer.HostID] = struct{}{}
	}
	edgeIDs := map[string]struct{}{}
	for _, edge := range s.Edges {
		if edge.EdgeID == "" || edge.FromHostID == "" || edge.ToHostID == "" || edge.FromNodeID == "" || edge.ToNodeID == "" || edge.FromPortID == "" || edge.ToPortID == "" {
			return errors.New("session_connect edge is incomplete")
		}
		if edge.FromHostID == edge.ToHostID {
			return fmt.Errorf("session_connect edge %q must cross hosts", edge.EdgeID)
		}
		if _, duplicate := edgeIDs[edge.EdgeID]; duplicate {
			return fmt.Errorf("session_connect repeats edge %q", edge.EdgeID)
		}
		edgeIDs[edge.EdgeID] = struct{}{}
		fromLocal, toLocal := edge.FromHostID == s.HostID, edge.ToHostID == s.HostID
		if fromLocal == toLocal {
			return fmt.Errorf("session_connect edge %q must have exactly one local endpoint", edge.EdgeID)
		}
		otherHost := edge.FromHostID
		if fromLocal {
			otherHost = edge.ToHostID
		}
		if _, expected := peers[otherHost]; !expected {
			return fmt.Errorf("session_connect edge %q references unexpected peer %q", edge.EdgeID, otherHost)
		}
	}
	return nil
}

func (s SessionReady) Validate() error {
	if s.ABI != ABIVersion {
		return fmt.Errorf("agent reports ABI %q, this service speaks %q", s.ABI, ABIVersion)
	}
	return nil
}

func (c Credit) Validate() error {
	if c.EdgeID == "" && c.PortID == "" {
		return errors.New("credit requires an edge or port ID")
	}
	if c.Batches == 0 {
		return errors.New("credit requires a positive batch count")
	}
	if c.Batches > MaxSessionEdges {
		return errors.New("credit batch count exceeds its limit")
	}
	return nil
}
