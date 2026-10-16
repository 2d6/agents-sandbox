package volume

import (
	"context"
	"reflect"
	"testing"

	msbSdk "github.com/superradcompany/microsandbox/sdk/go"

	"github.com/inoio/agents-sandbox/internal/configpaths"
	"github.com/inoio/agents-sandbox/internal/sandbox/image"
	"github.com/inoio/agents-sandbox/internal/sandbox/mounts"
	"github.com/inoio/agents-sandbox/internal/sandbox/msb"
	"github.com/inoio/agents-sandbox/internal/sandbox/network"
	"github.com/inoio/agents-sandbox/internal/sandbox/state"
	"github.com/inoio/agents-sandbox/internal/termio"
)

// TestEnsureNewHomeSeedsFingerprints pins that a fresh home volume's state
// carries the desired config fingerprints, so it is never fingerprint-less
// beside the VM created from it (issue #183).
func TestEnsureNewHomeSeedsFingerprints(t *testing.T) {
	configpaths.WithMockConfigPaths(t)

	client := &msb.MockMsbClient{
		CreateVolumeFn: func(_ context.Context, name string, _ ...msbSdk.VolumeOption) (msb.VolumeHandle, error) {
			return msb.MockVolumeHandle{Name_: name}, nil
		},
	}
	ui := &termio.Mock{}
	k := state.Key{Slug: "seedproj", Agent: "opencode"}
	desired := state.Fingerprints{
		Network: state.BuildNetworkState(network.Policy{Profile: network.ProfileNone}),
		Mount:   state.BuildMountState(mounts.Mounts{"/host/data": {Source: "/host/data"}}),
	}

	_, hs, err := NewManager(ui).EnsureNewHome(
		context.Background(),
		client,
		k,
		image.ImageInfo{Tag: "img:tag", Digest: "sha256:digest"},
		true, // dryRunVM
		desired,
		ui,
	)
	if err != nil {
		t.Fatalf("EnsureNewHome: %v", err)
	}
	if !reflect.DeepEqual(hs.NetworkState, desired.Network) || !reflect.DeepEqual(hs.MountState, desired.Mount) {
		t.Errorf("returned state not seeded: got %+v, want %+v", hs, desired)
	}

	onDisk, err := state.ReadState(k)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if !reflect.DeepEqual(onDisk.NetworkState, desired.Network) ||
		!reflect.DeepEqual(onDisk.MountState, desired.Mount) {
		t.Errorf("persisted state not seeded: got %+v, want %+v", onDisk, desired)
	}
}

// TestResolveHomeVolumeExistingStateKeepsFingerprints pins that the existing
// path is not reseeded: a stored fingerprint (possibly stale) must survive so
// the reconfig decision can detect a real change.
func TestResolveHomeVolumeExistingStateKeepsFingerprints(t *testing.T) {
	configpaths.WithMockConfigPaths(t)

	client := &msb.MockMsbClient{
		GetVolumeFn: func(_ context.Context, name string) (msb.VolumeHandle, error) {
			return msb.MockVolumeHandle{Name_: name}, nil
		},
	}
	ui := &termio.Mock{}
	k := state.Key{Slug: "existingproj", Agent: "opencode"}
	stored := state.BuildNetworkState(
		network.Policy{Profile: network.ProfilePublic, EgressAllow: []string{"example.com"}},
	)
	if err := state.WriteState(k, state.HomeState{
		HomeVolume:   "home-vol",
		ImageDigest:  "sha256:abc",
		NetworkState: stored,
	}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	desired := state.Fingerprints{
		Network: state.BuildNetworkState(network.Policy{Profile: network.ProfileNone}),
	}
	_, hs, err := NewManager(ui).ResolveHomeVolume(
		context.Background(),
		client,
		k,
		image.ImageInfo{Tag: "img:tag", Digest: "sha256:abc"},
		true,
		desired,
		ui,
	)
	if err != nil {
		t.Fatalf("ResolveHomeVolume: %v", err)
	}
	if !reflect.DeepEqual(hs.NetworkState, stored) {
		t.Errorf("existing fingerprint was overwritten: got %+v, want %+v", hs.NetworkState, stored)
	}
}
