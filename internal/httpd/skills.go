package httpd

import (
	"encoding/json"
	"net/http"

	"github.com/luohui1/MCP-cell/internal/skills"
)

// The Skill page uses the same package as `mcpcell skills`; the two work on
// the files directly and a lock file in the MCPcell home keeps them apart.

func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	if s.opts.Skills == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Skill 管理不可用"})
		return
	}
	st, err := s.opts.Skills.Scan()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if st.Skills == nil {
		st.Skills = []skills.Skill{}
	}
	writeJSON(w, http.StatusOK, st)
}

type skillSyncRequest struct {
	Names  []string `json:"names"`
	All    bool     `json:"all"`
	To     []string `json:"to"`
	From   string   `json:"from"`
	Copy   bool     `json:"copy"`
	DryRun bool     `json:"dryRun"`
}

type skillRestoreRequest struct {
	Names     []string `json:"names"`
	All       bool     `json:"all"`
	Locations []string `json:"locations"`
	DryRun    bool     `json:"dryRun"`
}

func (s *Server) handleSkillSync(w http.ResponseWriter, r *http.Request) {
	var req skillSyncRequest
	if !s.readSkillRequest(w, r, &req) {
		return
	}
	res, err := s.opts.Skills.Sync(skills.SyncOptions{Names: req.Names, All: req.All, To: req.To,
		From: req.From, Copy: req.Copy, DryRun: req.DryRun})
	s.writeSkillResult(w, "skills synced", res, err)
}

func (s *Server) handleSkillRestore(w http.ResponseWriter, r *http.Request) {
	var req skillRestoreRequest
	if !s.readSkillRequest(w, r, &req) {
		return
	}
	res, err := s.opts.Skills.Restore(skills.RestoreOptions{Names: req.Names, All: req.All,
		Locations: req.Locations, DryRun: req.DryRun})
	s.writeSkillResult(w, "skills restored", res, err)
}

func (s *Server) readSkillRequest(w http.ResponseWriter, r *http.Request, v any) bool {
	if s.opts.Skills == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Skill 管理不可用"})
		return false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return false
	}
	return true
}

func (s *Server) writeSkillResult(w http.ResponseWriter, msg string, res *skills.Result, err error) {
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if res.Actions == nil {
		res.Actions = []skills.Action{}
	}
	if !res.DryRun && res.Changes() > 0 {
		s.opts.Log.Info(msg, "changes", res.Changes())
	}
	writeJSON(w, http.StatusOK, res)
}
