package api

import (
	"fmt"
	"net/http"

	"glm5.2proxy/internal/zcodeenv"
)

func (s *Server) zcodeEnvironment(w http.ResponseWriter, _ *http.Request) {
	env := zcodeenv.Detect()
	writeJSON(w, http.StatusOK, map[string]any{"object": "zcode.environment", "data": env})
}

func (s *Server) activateAccountInZCode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	account := s.accounts.Get(id)
	if account == nil {
		writeError(w, http.StatusNotFound, "account not found", "not_found")
		return
	}
	result, err := zcodeenv.ApplyAccountWithBridge(*account, fmt.Sprintf("http://127.0.0.1:%d", s.port))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "zcode_environment_apply_failed")
		return
	}
	command := s.zcode.QueueRefresh(result.Account.ID, result.Account.Label)
	result.LiveRefreshQueued = true
	if result.BridgePatched {
		s.logs.add("info", "zcode.bridge_patched", result.BridgePatchMessage)
	}
	s.logs.add("info", "zcode.account_applied", "Conta "+result.Account.Label+" gravada no ambiente interno do ZCode; refresh live enfileirado em "+command.CommandID)
	writeJSON(w, http.StatusOK, map[string]any{"object": "zcode.account_applied", "data": result})
}

func (s *Server) applyAccountInZCode(accountID string) (*zcodeenv.ApplyResult, error) {
	env := zcodeenv.Detect()
	if !zcodeenv.Available(env) {
		s.logs.add("info", "zcode.environment_missing", "Conta ativada no proxy; ambiente interno do ZCode nao foi detectado, entao a sincronizacao foi ignorada")
		return nil, nil
	}
	account := s.accounts.Get(accountID)
	if account == nil {
		return nil, nil
	}
	result, err := zcodeenv.ApplyAccountWithBridge(*account, fmt.Sprintf("http://127.0.0.1:%d", s.port))
	if err != nil {
		s.logs.add("warn", "zcode.account_apply_failed", "Conta "+account.ID+" ativada no proxy, mas nao foi aplicada no ZCode: "+err.Error())
		return nil, err
	}
	command := s.zcode.QueueRefresh(result.Account.ID, result.Account.Label)
	result.LiveRefreshQueued = true
	if result.BridgePatched {
		s.logs.add("info", "zcode.bridge_patched", result.BridgePatchMessage)
	}
	s.logs.add("info", "zcode.account_applied", "Conta "+result.Account.Label+" sincronizada no disco do ZCode; refresh live enfileirado em "+command.CommandID)
	return &result, nil
}
