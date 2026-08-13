package v1

import (
	"strings"
	"testing"
)

func TestInstalledRemoteSessionRequiresGrantedSecrets(t *testing.T) {
	start := SessionStart{
		ExecutionID: "run",
		Nodes: []SessionNode{{
			NodeID: "node", PackageID: "example.runner", Version: "1.0.0",
			PackageDigest: strings.Repeat("a", 64),
			Capabilities:  []Capability{CapabilitySecret},
			RunnerGrant:   &RunnerGrant{NodeID: "node", Secrets: []string{"TOKEN"}},
			SecretValues:  map[string]string{"OTHER": "value"},
		}},
	}
	if err := start.Validate(); err == nil || !strings.Contains(err.Error(), "ungranted secret") {
		t.Fatalf("validation error = %v", err)
	}
	start.Nodes[0].SecretValues = map[string]string{"TOKEN": "value"}
	if err := start.Validate(); err != nil {
		t.Fatalf("valid remote secret grant: %v", err)
	}
}

func TestSessionValidatesTrustedSpillLimits(t *testing.T) {
	limits := DefaultSpillLimits()
	start := SessionStart{ExecutionID: "run", Nodes: []SessionNode{{NodeID: "sort", PackageID: "com.dbminer.sort-field", Version: "0.1.0", SpillLimits: &limits}}}
	if err := start.Validate(); err != nil {
		t.Fatal(err)
	}
	limits.ScratchFiles = 3
	if err := start.Validate(); err == nil || !strings.Contains(err.Error(), "four scratch files") {
		t.Fatalf("error = %v", err)
	}
	limits = DefaultSpillLimits()
	start.Nodes[0].PackageDigest = strings.Repeat("a", 64)
	if err := start.Validate(); err == nil || !strings.Contains(err.Error(), "trusted spill limits") {
		t.Fatalf("installed error = %v", err)
	}
}

func TestSessionStartValidatesPeerTopology(t *testing.T) {
	start := SessionStart{
		ExecutionID: "run",
		HostID:      "host-a",
		Nodes:       []SessionNode{{NodeID: "left", PackageID: "example.runner", Version: "1"}},
		PeerTLS:     &SessionTLS{CA: []byte("ca"), Certificate: []byte("cert"), PrivateKey: []byte("key")},
		PeerEdges: []SessionPeerEdge{{
			EdgeID: "edge", FromHostID: "host-a", FromNodeID: "left", FromPortID: "out",
			ToHostID: "host-b", ToNodeID: "right", ToPortID: "in",
		}},
	}
	if err := start.Validate(); err != nil {
		t.Fatalf("valid peer topology: %v", err)
	}

	wrongHost := start
	wrongHost.PeerEdges = append([]SessionPeerEdge(nil), start.PeerEdges...)
	wrongHost.PeerEdges[0].FromHostID = "host-c"
	if err := wrongHost.Validate(); err == nil || !strings.Contains(err.Error(), "wrong local host") {
		t.Fatalf("wrong-host error = %v", err)
	}

	duplicate := start
	duplicate.Boundaries = []SessionBoundary{{EdgeID: "edge", NodeID: "left", PortID: "out", Direction: BoundaryOutput}}
	if err := duplicate.Validate(); err == nil || !strings.Contains(err.Error(), "repeats edge") {
		t.Fatalf("duplicate-edge error = %v", err)
	}
}

func TestSessionConnectRejectsMalformedAndDuplicateTopology(t *testing.T) {
	connect := SessionConnect{
		HostID: "host-a",
		Peers:  []SessionPeer{{HostID: "host-b", Address: "192.0.2.1", Port: 3210}},
		Edges:  []SessionPeerEdge{{EdgeID: "edge", FromHostID: "host-a", FromNodeID: "left", FromPortID: "out", ToHostID: "host-b", ToNodeID: "right", ToPortID: "in"}},
	}
	if err := connect.Validate(); err != nil {
		t.Fatalf("valid connect topology: %v", err)
	}
	duplicate := connect
	duplicate.Edges = append(append([]SessionPeerEdge(nil), connect.Edges...), connect.Edges[0])
	if err := duplicate.Validate(); err == nil || !strings.Contains(err.Error(), "repeats edge") {
		t.Fatalf("duplicate error = %v", err)
	}
	unexpected := connect
	unexpected.Edges = append([]SessionPeerEdge(nil), connect.Edges...)
	unexpected.Edges[0].FromHostID = "host-c"
	if err := unexpected.Validate(); err == nil || !strings.Contains(err.Error(), "exactly one local endpoint") {
		t.Fatalf("unexpected-peer error = %v", err)
	}
}

func TestSessionPreviewIsBounded(t *testing.T) {
	value := "value"
	rows := make([][]*string, 1_001)
	for index := range rows {
		rows[index] = []*string{&value}
	}
	preview := SessionPreview{Ports: []SessionPreviewPort{{PortID: "output", Columns: []SessionPreviewColumn{{Name: "value", Type: "string"}}, Rows: rows, RowsProduced: uint64(len(rows))}}}
	if err := preview.Validate(); err == nil || !strings.Contains(err.Error(), "bounded snapshot") {
		t.Fatalf("row-limit error = %v", err)
	}
	preview.Ports[0].Rows = rows[:1]
	preview.Ports[0].Rows[0] = []*string{&value, &value}
	if err := preview.Validate(); err == nil || !strings.Contains(err.Error(), "row width") {
		t.Fatalf("row-width error = %v", err)
	}
}
