package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"lifeagent/internal/appcat"
)

const (
	settingCategorize = "categorize"
	categorizeMax     = 100 // names in one request
)

// categorizeSettings is stored under the key "categorize". Sending names to
// the model is on unless the user switches it off.
type categorizeSettings struct {
	LLM bool `json:"llm"`
}

func parseCategorizeSettings(raw []byte) (categorizeSettings, error) {
	s := categorizeSettings{LLM: true}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return categorizeSettings{}, err
	}
	return s, nil
}

func (s *Server) categorizeSettings() (categorizeSettings, error) {
	kv, err := s.store.Settings()
	if err != nil {
		return categorizeSettings{}, err
	}
	if raw, ok := kv[settingCategorize]; ok {
		return parseCategorizeSettings([]byte(raw))
	}
	return categorizeSettings{LLM: true}, nil
}

type categorizeResult struct {
	appcatResult
	Invalid bool `json:"invalid,omitempty"`
}

type appcatResult = appcat.Result

// postCategorize turns names of apps, games and domains into categories.
// Names are the only usage-related data that may leave the phone or the Mac,
// and only the ones nothing local could place. Nothing here is logged.
func (s *Server) postCategorize(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	var in struct {
		Names []string `json:"names"`
		// LLM false: the cache and the dictionary only, no model call for this
		// request (the app asks the model only for names left after the context layer).
		LLM *bool `json:"llm"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || len(in.Names) == 0 || len(in.Names) > categorizeMax {
		writeError(w, http.StatusBadRequest, `body must be {"names": [1..100 names]}`)
		return
	}
	set, err := s.categorizeSettings()
	if err != nil {
		s.fail(w, err)
		return
	}

	out := make(map[string]categorizeResult, len(in.Names))
	var keys []string
	keyOf := map[string]string{} // original name -> normalized
	for _, name := range in.Names {
		k, ok := appcat.NormalizeKey(name)
		if !ok {
			out[name] = categorizeResult{appcatResult: appcat.Result{Category: "unknown"}, Invalid: true}
			continue
		}
		keyOf[name] = k
		keys = append(keys, k)
	}
	if len(keys) > 0 {
		minQuota := 10
		if a, err := s.adviceSettings(); err == nil {
			minQuota = a.MinQuota
		}
		res := &appcat.Resolver{Store: s.store, LLM: s.caller, Loc: s.loc, Now: s.now, MinQuota: minQuota}
		if s.llmInfo != nil {
			res.Quota = s.llmInfo.Quota
		}
		allow := set.LLM && (in.LLM == nil || *in.LLM)
		got, err := res.Resolve(r.Context(), keys, allow)
		if err != nil {
			s.fail(w, err)
			return
		}
		for name, k := range keyOf {
			out[name] = categorizeResult{appcatResult: got[k]}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"categories": out})
}
