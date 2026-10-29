package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"lifeagent/internal/store"
)

func newAppServer(t *testing.T, dir string) http.Handler {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(Options{
		Store: st, Token: testToken, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		RatePerSec: 100, Burst: 100, AppDir: dir,
	})
}

func writeRelease(t *testing.T, dir string, apk []byte, size int64) string {
	t.Helper()
	sum := sha256.Sum256(apk)
	hexSum := hex.EncodeToString(sum[:])
	if err := os.WriteFile(filepath.Join(dir, appAPKName), apk, 0o644); err != nil {
		t.Fatal(err)
	}
	latest := fmt.Sprintf(`{"version_code":7,"version_name":"1.0.0","sha256":%q,"size":%d}`, hexSum, size)
	if err := os.WriteFile(filepath.Join(dir, appLatestName), []byte(latest), 0o644); err != nil {
		t.Fatal(err)
	}
	return hexSum
}

func TestAppLatestAndAPK(t *testing.T) {
	dir := t.TempDir()
	apk := []byte("PK\x03\x04 not really an apk")
	sum := writeRelease(t, dir, apk, int64(len(apk)))
	h := newAppServer(t, dir)

	rec := do(h, "GET", "/v1/app/latest", "Bearer "+testToken, "")
	if rec.Code != 200 {
		t.Fatalf("latest: status %d %s", rec.Code, rec.Body)
	}
	var rel AppRelease
	if err := json.Unmarshal(rec.Body.Bytes(), &rel); err != nil {
		t.Fatal(err)
	}
	if rel.VersionCode != 7 || rel.VersionName != "1.0.0" || rel.SHA256 != sum || rel.Size != int64(len(apk)) {
		t.Fatalf("release = %+v", rel)
	}

	rec = do(h, "GET", "/v1/app/apk", "Bearer "+testToken, "")
	if rec.Code != 200 {
		t.Fatalf("apk: status %d", rec.Code)
	}
	if rec.Body.String() != string(apk) {
		t.Fatalf("apk body = %q", rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/vnd.android.package-archive" {
		t.Errorf("content type %q", ct)
	}
}

func TestAppNeedsToken(t *testing.T) {
	dir := t.TempDir()
	writeRelease(t, dir, []byte("apk"), 3)
	h := newAppServer(t, dir)
	for _, path := range []string{"/v1/app/latest", "/v1/app/apk"} {
		if rec := do(h, "GET", path, "", ""); rec.Code != 401 {
			t.Errorf("%s without token: %d", path, rec.Code)
		}
	}
}

func TestAppNoRelease(t *testing.T) {
	half := t.TempDir()
	// deploy.sh is still copying: the APK is shorter than latest.json says.
	writeRelease(t, half, []byte("apk"), 10)
	for name, dir := range map[string]string{"no dir": "", "empty dir": t.TempDir(), "partial copy": half} {
		h := newAppServer(t, dir)
		for _, path := range []string{"/v1/app/latest", "/v1/app/apk"} {
			rec := do(h, "GET", path, "Bearer "+testToken, "")
			if rec.Code != 404 {
				t.Errorf("%s %s: status %d", name, path, rec.Code)
			}
		}
	}
}

func TestAppBadLatest(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, appAPKName), []byte("apk"), 0o644)
	os.WriteFile(filepath.Join(dir, appLatestName), []byte(`{"version_code":0,"sha256":"x","size":3}`), 0o644)
	h := newAppServer(t, dir)
	if rec := do(h, "GET", "/v1/app/latest", "Bearer "+testToken, ""); rec.Code != 500 {
		t.Fatalf("status %d", rec.Code)
	}
}
