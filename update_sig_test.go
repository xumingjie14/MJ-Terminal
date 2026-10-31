package main

// v0.61 update-manifest signature tests: the check channels
// must refuse -- phase "error", no offer, a message naming the
// signature -- whenever the detached Ed25519 signature over
// update.json is missing, unfetchable, made by the wrong key,
// or made over different bytes. A valid signature lets the
// pre-existing offer logic proceed untouched. The fixture
// (update_test.go) signs with a throwaway key whose public
// half stands in for the embedded release key.

import (
	"strings"
	"testing"
)

// officialSigFixture wires a "latest official release" at the
// version one minor above the running build, with manifest and
// signature asset URLs attached (unless withSigAsset is false,
// which models a release published before signing existed).
func officialSigFixture(t *testing.T, sigMode string, withSigAsset bool) string {
	t.Helper()
	dsTestRoot(t)
	resetUpd(t)
	nv := nextVersion()
	f := newBetaFixture(t)
	lat := fixtureRelease("v"+nv, false)
	if withSigAsset {
		f.attachSignedAssets(&lat)
	} else {
		dl := f.srv.URL + "/" + updateRepo + "/releases/download/v" + nv + "/"
		lat.Assets = append(lat.Assets, struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		}{Name: updateManifestName, URL: dl + updateManifestName})
	}
	f.lat = &lat
	f.mans["v"+nv] = updateManifest{Version: nv, File: updateZipName, Size: 62_000_000, SHA256: "ab"}
	if sigMode != "" {
		f.sigMode["v"+nv] = sigMode
	}
	return nv
}

func TestOfficialCheckValidSignatureOffers(t *testing.T) {
	nv := officialSigFixture(t, "", true)
	checkUpdateOfficial()
	snap := upd.snapshot()
	if snap.Phase != "available" || snap.Latest != nv {
		t.Fatalf("a validly signed newer manifest must be offered: %+v", snap)
	}
}

func TestOfficialCheckTamperedManifestRefused(t *testing.T) {
	officialSigFixture(t, "tampered", true)
	checkUpdateOfficial()
	snap := upd.snapshot()
	if snap.Phase != "error" || !strings.Contains(snap.Message, "签名") {
		t.Fatalf("a signature over different bytes must hard-refuse: %+v", snap)
	}
	if upd.zipAssetURL != "" || upd.Latest != "" {
		t.Fatalf("a refused manifest must leave no offer state behind: %+v", upd.snapshot())
	}
}

func TestOfficialCheckMissingSigAssetRefused(t *testing.T) {
	officialSigFixture(t, "", false)
	checkUpdateOfficial()
	snap := upd.snapshot()
	if snap.Phase != "error" || !strings.Contains(snap.Message, "缺少数字签名") {
		t.Fatalf("a release without update.json.sig must hard-refuse: %+v", snap)
	}
}

func TestOfficialCheckUnfetchableSigRefused(t *testing.T) {
	officialSigFixture(t, "missing", true)
	checkUpdateOfficial()
	snap := upd.snapshot()
	if snap.Phase != "error" || !strings.Contains(snap.Message, "签名") {
		t.Fatalf("an unfetchable signature must hard-refuse: %+v", snap)
	}
}

func TestOfficialCheckWrongKeyRefused(t *testing.T) {
	officialSigFixture(t, "wrongkey", true)
	checkUpdateOfficial()
	snap := upd.snapshot()
	if snap.Phase != "error" || !strings.Contains(snap.Message, "签名") {
		t.Fatalf("a signature from a different key must hard-refuse: %+v", snap)
	}
}

// betaSigFixture wires a prerelease at the next version with
// the given signature mode, on the beta channel's reads.
func betaSigFixture(t *testing.T, sigMode string) string {
	t.Helper()
	dsTestRoot(t)
	resetUpd(t)
	nv := nextVersion()
	f := newBetaFixture(t)
	f.list = []ghRelease{fixtureRelease("v"+nv+"-test1", true)}
	f.mans["v"+nv+"-test1"] = updateManifest{Version: nv, File: updateZipName, Size: 62_000_000, SHA256: "ab"}
	if sigMode != "" {
		f.sigMode["v"+nv+"-test1"] = sigMode
	}
	return nv
}

func TestBetaCheckTamperedManifestRefused(t *testing.T) {
	betaSigFixture(t, "tampered")
	checkUpdateBeta()
	snap := upd.snapshotLocal()
	if snap.Phase != "error" || !strings.Contains(snap.Message, "签名") {
		t.Fatalf("beta: a signature over different bytes must hard-refuse: %+v", snap)
	}
}

func TestBetaCheckMissingSigRefused(t *testing.T) {
	betaSigFixture(t, "missing")
	checkUpdateBeta()
	snap := upd.snapshotLocal()
	if snap.Phase != "error" || !strings.Contains(snap.Message, "签名") {
		t.Fatalf("beta: an unfetchable signature must hard-refuse: %+v", snap)
	}
}

func TestBetaCheckWrongKeyRefused(t *testing.T) {
	betaSigFixture(t, "wrongkey")
	checkUpdateBeta()
	snap := upd.snapshotLocal()
	if snap.Phase != "error" || !strings.Contains(snap.Message, "签名") {
		t.Fatalf("beta: a signature from a different key must hard-refuse: %+v", snap)
	}
}

// The verify entry point itself: garbage of any shape is a
// plain false, never a panic, under the real embedded key.
func TestVerifyManifestSignatureGarbage(t *testing.T) {
	mb := []byte(`{"version":"9.99","file":"x","size":1,"sha256":"ab"}`)
	for _, sig := range [][]byte{
		nil,
		{},
		{0},
		make([]byte, 63),
		make([]byte, 64), // right size, all zero
		make([]byte, 65),
		[]byte("not a signature at all, just text padding to be long enough to matter....."),
	} {
		if verifyManifestSignature(mb, sig) {
			t.Fatalf("garbage signature (%d bytes) must not verify", len(sig))
		}
	}
	if verifyManifestSignature(nil, nil) {
		t.Fatal("empty manifest + empty signature must not verify")
	}
}
