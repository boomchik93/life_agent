package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// The phone app updates itself from the server: deploy.sh puts the signed APK
// and its description into AppDir, the app asks for the description, compares
// the version code with its own and downloads the APK with the same token.
const (
	appAPKName    = "life-agent.apk"
	appLatestName = "latest.json"
	// A slow mobile network: the APK is tens of megabytes.
	appDownloadTimeout = 10 * time.Minute
)

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// AppRelease describes the APK in AppDir.
type AppRelease struct {
	VersionCode int64  `json:"version_code"`
	VersionName string `json:"version_name"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
}

var errNoRelease = errors.New("no release")

// appRelease reads latest.json and checks that the APK next to it is the one
// it describes; while deploy.sh is still copying, the sizes differ.
func (s *Server) appRelease() (AppRelease, error) {
	if s.appDir == "" {
		return AppRelease{}, errNoRelease
	}
	raw, err := os.ReadFile(filepath.Join(s.appDir, appLatestName))
	if errors.Is(err, os.ErrNotExist) {
		return AppRelease{}, errNoRelease
	}
	if err != nil {
		return AppRelease{}, err
	}
	var r AppRelease
	if err := json.Unmarshal(raw, &r); err != nil {
		return AppRelease{}, err
	}
	if r.VersionCode <= 0 || r.Size <= 0 || !sha256Hex.MatchString(r.SHA256) {
		return AppRelease{}, errors.New("latest.json: bad version_code, size or sha256")
	}
	st, err := os.Stat(filepath.Join(s.appDir, appAPKName))
	if errors.Is(err, os.ErrNotExist) || (err == nil && st.Size() != r.Size) {
		return AppRelease{}, errNoRelease
	}
	if err != nil {
		return AppRelease{}, err
	}
	return r, nil
}

func (s *Server) getAppLatest(w http.ResponseWriter, r *http.Request) {
	rel, err := s.appRelease()
	if errors.Is(err, errNoRelease) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no release", "code": "no_release"})
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rel)
}

func (s *Server) getAppAPK(w http.ResponseWriter, r *http.Request) {
	rel, err := s.appRelease()
	if errors.Is(err, errNoRelease) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no release", "code": "no_release"})
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	f, err := os.Open(filepath.Join(s.appDir, appAPKName))
	if err != nil {
		s.fail(w, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		s.fail(w, err)
		return
	}
	// The server's write timeout is sized for JSON answers, not for an APK.
	http.NewResponseController(w).SetWriteDeadline(time.Now().Add(appDownloadTimeout))
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("X-Content-SHA256", rel.SHA256)
	http.ServeContent(w, r, appAPKName, st.ModTime(), f)
}
