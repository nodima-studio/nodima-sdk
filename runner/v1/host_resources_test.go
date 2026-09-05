package v1

import "testing"

func TestHostResourcesMessageValidatesWholeComputerMemory(t *testing.T) {
	message := NewMessage(MessageHostResources)
	message.HostResources = &HostResources{Memory: &MemoryResources{TotalBytes: 16 << 30, UsedBytes: 6 << 30}, Start: true}
	if err := message.Validate(); err != nil {
		t.Fatal(err)
	}

	message.HostResources.End = true
	if err := message.Validate(); err == nil {
		t.Fatal("message validated an impossible start/end sample")
	}
}
