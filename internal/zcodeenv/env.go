package zcodeenv

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"glm5.2proxy/internal/accounts"
)

const (
	credentialActiveProvider = "oauth:active_provider"
	credentialAccessToken    = "oauth:zai:access_token"
	credentialUserInfo       = "oauth:zai:user_info"
	credentialJWTToken       = "zcodejwttoken"
)

type Environment struct {
	HomeDir             string       `json:"homeDir"`
	DataDir             string       `json:"dataDir"`
	CredentialsPath     string       `json:"credentialsPath"`
	ConfigPath          string       `json:"configPath"`
	SettingPath         string       `json:"settingPath"`
	CodingPlanPath      string       `json:"codingPlanPath"`
	InstallPath         string       `json:"installPath,omitempty"`
	AppServerScript     string       `json:"appServerScript,omitempty"`
	RunningProcesses    []Process    `json:"runningProcesses"`
	CurrentUser         *CurrentUser `json:"currentUser,omitempty"`
	CredentialsPresent  bool         `json:"credentialsPresent"`
	ConfigPresent       bool         `json:"configPresent"`
	DetectedAt          time.Time    `json:"detectedAt"`
	RestartRecommended  bool         `json:"restartRecommended"`
	LiveRefreshPossible bool         `json:"liveRefreshPossible"`
	LiveRefreshReason   string       `json:"liveRefreshReason,omitempty"`
	Warnings            []string     `json:"warnings,omitempty"`
}

type Process struct {
	PID         int    `json:"pid"`
	Executable  string `json:"executable,omitempty"`
	CommandLine string `json:"commandLine,omitempty"`
	Role        string `json:"role"`
}

type CurrentUser struct {
	ID    string `json:"id,omitempty"`
	Email string `json:"email,omitempty"`
	Name  string `json:"name,omitempty"`
}

type ApplyResult struct {
	Environment         Environment            `json:"environment"`
	Account             accounts.PublicAccount `json:"account"`
	BackupPath          string                 `json:"backupPath,omitempty"`
	ConfigUpdated       bool                   `json:"configUpdated"`
	CredentialsUpdated  bool                   `json:"credentialsUpdated"`
	RestartRecommended  bool                   `json:"restartRecommended"`
	LiveRefreshPossible bool                   `json:"liveRefreshPossible"`
	LiveRefreshReason   string                 `json:"liveRefreshReason,omitempty"`
	LiveRefreshQueued   bool                   `json:"liveRefreshQueued"`
}

func Detect() Environment {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".zcode", "v2")
	env := Environment{
		HomeDir:             home,
		DataDir:             dataDir,
		CredentialsPath:     filepath.Join(dataDir, "credentials.json"),
		ConfigPath:          filepath.Join(dataDir, "config.json"),
		SettingPath:         filepath.Join(dataDir, "setting.json"),
		CodingPlanPath:      filepath.Join(dataDir, "coding-plan-cache.json"),
		DetectedAt:          time.Now(),
		RestartRecommended:  true,
		LiveRefreshPossible: false,
		LiveRefreshReason:   "Sem o patch bridge do renderer, o refresh de conta fica atras de um RPC privado por Electron MessagePort. Com o patch instalado, o proxy enfileira o refresh e o renderer do ZCode executa internamente.",
	}
	env.RunningProcesses = runningProcesses()
	for _, process := range env.RunningProcesses {
		if env.InstallPath == "" && strings.HasSuffix(strings.ToLower(process.Executable), "zcode.exe") {
			env.InstallPath = process.Executable
		}
		if env.AppServerScript == "" && strings.Contains(strings.ToLower(process.CommandLine), `resources\glm\zcode.cjs`) {
			env.AppServerScript = filepath.Join(filepath.Dir(filepath.Dir(process.Executable)), "resources", "glm", "zcode.cjs")
			if _, err := os.Stat(env.AppServerScript); err != nil {
				env.AppServerScript = ""
			}
		}
	}
	if env.InstallPath == "" && runtime.GOOS == "windows" {
		candidate := filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "ZCode", "ZCode.exe")
		if _, err := os.Stat(candidate); err == nil {
			env.InstallPath = candidate
		}
	}
	if env.AppServerScript == "" && env.InstallPath != "" {
		candidate := filepath.Join(filepath.Dir(env.InstallPath), "resources", "glm", "zcode.cjs")
		if _, err := os.Stat(candidate); err == nil {
			env.AppServerScript = candidate
		}
	}
	env.CredentialsPresent = fileExists(env.CredentialsPath)
	env.ConfigPresent = fileExists(env.ConfigPath)
	if env.CredentialsPresent {
		if current, err := readCurrentUser(env.CredentialsPath, NewCipher(home)); err == nil {
			env.CurrentUser = current
		} else {
			env.Warnings = append(env.Warnings, "Nao foi possivel descriptografar o usuario atual do ZCode: "+err.Error())
		}
	}
	sort.SliceStable(env.RunningProcesses, func(i, j int) bool { return env.RunningProcesses[i].PID < env.RunningProcesses[j].PID })
	return env
}

func Available(env Environment) bool {
	return env.InstallPath != "" || env.AppServerScript != "" || env.CredentialsPresent || env.ConfigPresent || len(env.RunningProcesses) > 0
}

func ApplyAccount(account accounts.Account) (ApplyResult, error) {
	env := Detect()
	if account.ZCodeJWTToken == "" {
		return ApplyResult{}, errors.New("conta sem zcodeJwtToken salvo")
	}
	if err := os.MkdirAll(env.DataDir, 0o700); err != nil {
		return ApplyResult{}, err
	}
	cipher := NewCipher(env.HomeDir)
	backup, err := writeCredentials(env.CredentialsPath, cipher, account)
	if err != nil {
		return ApplyResult{}, err
	}
	configUpdated, err := updateConfig(env.ConfigPath, account.ZCodeJWTToken)
	if err != nil {
		return ApplyResult{}, err
	}
	env = Detect()
	return ApplyResult{
		Environment:         env,
		Account:             accounts.Sanitize(account),
		BackupPath:          backup,
		ConfigUpdated:       configUpdated,
		CredentialsUpdated:  true,
		RestartRecommended:  true,
		LiveRefreshPossible: false,
		LiveRefreshReason:   env.LiveRefreshReason,
	}, nil
}

func writeCredentials(path string, cipher Cipher, account accounts.Account) (string, error) {
	credentials := map[string]string{}
	if raw, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(raw)) > 0 {
		_ = json.Unmarshal(raw, &credentials)
	}
	backup := ""
	if fileExists(path) {
		backup = path + ".glm5proxy-backup-" + time.Now().Format("20060102-150405")
		if raw, err := os.ReadFile(path); err == nil {
			if err := os.WriteFile(backup, raw, 0o600); err != nil {
				return "", err
			}
		}
	}
	userInfo, err := json.Marshal(account.User)
	if err != nil {
		return "", err
	}
	values := map[string]string{
		credentialActiveProvider: "zai",
		credentialUserInfo:       string(userInfo),
		credentialJWTToken:       account.ZCodeJWTToken,
	}
	if account.ZAIAcccessToken != "" {
		values[credentialAccessToken] = account.ZAIAcccessToken
	}
	for key, value := range values {
		encrypted, err := cipher.Encrypt(value)
		if err != nil {
			return "", err
		}
		credentials[key] = encrypted
	}
	raw, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return "", err
	}
	return backup, os.WriteFile(path, append(raw, '\n'), 0o600)
}

func updateConfig(path, jwt string) (bool, error) {
	config := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &config); err != nil {
			return false, err
		}
	}
	providers, _ := config["modelProviders"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
		config["modelProviders"] = providers
	}
	provider, _ := providers["builtin:zai-start-plan"].(map[string]any)
	if provider == nil {
		provider = map[string]any{}
		providers["builtin:zai-start-plan"] = provider
	}
	provider["enabled"] = true
	options, _ := provider["options"].(map[string]any)
	if options == nil {
		options = map[string]any{}
		provider["options"] = options
	}
	options["apiKey"] = jwt
	if _, ok := options["baseURL"]; !ok {
		options["baseURL"] = "https://zcode.z.ai/api/v1/zcode-plan/anthropic"
	}
	raw, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, append(raw, '\n'), 0o600)
}

func readCurrentUser(path string, cipher Cipher) (*CurrentUser, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	credentials := map[string]string{}
	if err := json.Unmarshal(raw, &credentials); err != nil {
		return nil, err
	}
	value := credentials[credentialUserInfo]
	if value == "" {
		return nil, nil
	}
	plain, err := cipher.Decrypt(value)
	if err != nil {
		return nil, err
	}
	var user accounts.User
	if err := json.Unmarshal([]byte(plain), &user); err != nil {
		return nil, err
	}
	return &CurrentUser{ID: first(user.UserID, user.ID), Email: user.Email, Name: first(user.Name, user.Nickname)}, nil
}

func runningProcesses() []Process {
	if runtime.GOOS != "windows" {
		return nil
	}
	command := exec.Command("powershell", "-NoProfile", "-Command", "Get-CimInstance Win32_Process -Filter \"name='ZCode.exe'\" | Select-Object ProcessId,ExecutablePath,CommandLine | ConvertTo-Csv -NoTypeInformation")
	output, err := command.Output()
	if err != nil {
		return nil
	}
	reader := csv.NewReader(bytes.NewReader(output))
	records, err := reader.ReadAll()
	if err != nil || len(records) < 2 {
		return nil
	}
	var processes []Process
	for _, record := range records[1:] {
		if len(record) < 3 {
			continue
		}
		pid := 0
		_, _ = fmt.Sscanf(record[0], "%d", &pid)
		commandLine := record[2]
		processes = append(processes, Process{PID: pid, Executable: record[1], CommandLine: commandLine, Role: processRole(commandLine)})
	}
	return processes
}

func processRole(commandLine string) string {
	lower := strings.ToLower(commandLine)
	switch {
	case strings.Contains(lower, "zcode.cjs app-server --stdio"):
		return "app-server"
	case strings.Contains(lower, "--type=renderer"):
		return "renderer"
	case strings.Contains(lower, "--type=utility"):
		return "utility"
	case strings.Contains(lower, "--type=gpu-process"):
		return "gpu"
	default:
		return "main"
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
