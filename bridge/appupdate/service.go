package appupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"guiforcores/bridge/config"
	"guiforcores/bridge/logging"
	"guiforcores/bridge/network"
	"guiforcores/bridge/platform"
	appv1 "guiforcores/gen/app/v1"

	connect "connectrpc.com/connect"
)

const (
	appTitle               = "webui.for.singbox"
	appUpdateCacheFilePath = "data/.cache/gui-update.zip"
	appUpdateHelperCommand = "__updater"
)

// Repository is injected by release builds so a fork updates from its own assets.
var Repository = "Ghostdehole/WEBUI.for.SingBox"
var newRequestClient = network.NewClient

type AppConfigReader interface {
	Current() config.AppConfig
}

type EventPublisher interface {
	Publish(eventName string, data ...any)
}

type Service struct {
	requestShutdown func()
	platform        *platform.Service
	appConfig       AppConfigReader
	events          EventPublisher
	currentVersion  string
	serviceMode     bool
	logLevel        logging.Level
	logDays         int

	mu             sync.Mutex
	applying       bool
	preparedUpdate string
	updatedVersion string
	downloads      map[string]context.CancelFunc
}

func (s *Service) SetShutdownHandler(handler func()) { s.requestShutdown = handler }

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

func NewService(
	platformService *platform.Service,
	appConfig AppConfigReader,
	events EventPublisher,
	currentVersion string,
	serviceMode bool,
	logLevel logging.Level,
	logDays int,
) *Service {
	currentVersion = strings.TrimSpace(currentVersion)
	if currentVersion == "" {
		currentVersion = "unknown"
	}
	if _, err := logging.ParseLevel(logLevel.String()); err != nil {
		logLevel = logging.LevelInfo
	}
	return &Service{
		platform:       platformService,
		appConfig:      appConfig,
		events:         events,
		currentVersion: currentVersion,
		serviceMode:    serviceMode,
		logLevel:       logLevel,
		logDays:        logDays,
		updatedVersion: currentVersion,
		downloads:      map[string]context.CancelFunc{},
	}
}

func (s *Service) GetAppVersion(
	_ context.Context,
	_ *connect.Request[appv1.GetAppVersionRequest],
) (*connect.Response[appv1.GetAppVersionResponse], error) {
	current, updated := s.versions()
	return connect.NewResponse(&appv1.GetAppVersionResponse{
		CurrentVersion: current,
		UpdatedVersion: updated,
	}), nil
}

func (s *Service) CheckAppUpdate(
	ctx context.Context,
	_ *connect.Request[appv1.CheckAppUpdateRequest],
) (response *connect.Response[appv1.CheckAppUpdateResponse], responseErr error) {
	started := time.Now()
	defer func() {
		latest := ""
		if response != nil {
			latest = response.Msg.GetLatestVersion()
		}
		logging.Complete(ctx, "app_update", "check", "application update checked", started, responseErr, "latest_version", latest)
	}()
	release, _, err := s.fetchLatestAsset(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	current, updated := s.versions()
	latest := normalizeVersion(release.TagName)
	return connect.NewResponse(&appv1.CheckAppUpdateResponse{
		CurrentVersion:            current,
		UpdatedVersion:            updated,
		LatestVersion:             latest,
		Updatable:                 latest != "" && latest != updated,
		DownloadedUpdateAvailable: current != updated && fileExists(s.platform.ResolvePath(appUpdateCacheFilePath)),
	}), nil
}

func (s *Service) DownloadAppUpdate(
	ctx context.Context,
	req *connect.Request[appv1.DownloadAppUpdateRequest],
) (response *connect.Response[appv1.DownloadAppUpdateResponse], responseErr error) {
	started := time.Now()
	progressEvent := strings.TrimSpace(req.Msg.GetProgressEvent())
	defer func() {
		logging.Complete(ctx, "app_update", "download", "application update downloaded", started, responseErr, "progress_event", progressEvent)
	}()
	if progressEvent == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("progress_event is required"))
	}

	downloadCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.releaseFailedHandoffLocked()
	if s.applying || len(s.downloads) != 0 {
		s.mu.Unlock()
		cancel()
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("an application update download or installation is already in progress"))
	}
	s.downloads[progressEvent] = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.downloads, progressEvent)
		s.mu.Unlock()
		cancel()
	}()
	// Reserve the single cache before looking up the release, and keep the
	// reservation until a canceled transfer has actually returned. Otherwise a
	// second request can replace the archive while an apply is preparing it.
	release, asset, err := s.fetchLatestAsset(downloadCtx)
	if err != nil {
		if downloadCtx.Err() != nil {
			return nil, updateContextError(downloadCtx.Err())
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	cachePath := s.platform.ResolvePath(appUpdateCacheFilePath)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := s.downloadAsset(downloadCtx, asset, cachePath, progressEvent); err != nil {
		if downloadCtx.Err() != nil {
			return nil, updateContextError(downloadCtx.Err())
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	latest := normalizeVersion(release.TagName)
	s.mu.Lock()
	s.updatedVersion = latest
	s.mu.Unlock()
	current, updated := s.versions()

	return connect.NewResponse(&appv1.DownloadAppUpdateResponse{
		CurrentVersion:            current,
		UpdatedVersion:            updated,
		LatestVersion:             latest,
		DownloadedUpdateAvailable: current != updated && fileExists(cachePath),
	}), nil
}

func (s *Service) CancelAppUpdate(
	ctx context.Context,
	req *connect.Request[appv1.CancelAppUpdateRequest],
) (*connect.Response[appv1.CancelAppUpdateResponse], error) {
	progressEvent := strings.TrimSpace(req.Msg.GetProgressEvent())
	if progressEvent != "" {
		s.mu.Lock()
		cancel := s.downloads[progressEvent]
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
	logging.FromContext(ctx).InfoContext(ctx, "application update download cancelled", "component", "app_update", "operation", "cancel_download", "progress_event", progressEvent, "result", "cancelled")
	return connect.NewResponse(&appv1.CancelAppUpdateResponse{}), nil
}

func (s *Service) ApplyAppUpdate(
	ctx context.Context,
	_ *connect.Request[appv1.ApplyAppUpdateRequest],
) (response *connect.Response[appv1.ApplyAppUpdateResponse], responseErr error) {
	started := time.Now()
	defer func() {
		logging.Complete(ctx, "app_update", "apply", "application update started", started, responseErr, "service_mode", s.serviceMode)
	}()
	s.mu.Lock()
	s.releaseFailedHandoffLocked()
	if len(s.downloads) != 0 {
		s.mu.Unlock()
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("application update download is still in progress"))
	}
	if s.applying {
		s.mu.Unlock()
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("application update is already being applied"))
	}
	s.applying = true
	s.mu.Unlock()
	handedOff := false
	defer func() {
		if !handedOff {
			s.mu.Lock()
			s.applying = false
			s.preparedUpdate = ""
			s.mu.Unlock()
		}
	}()
	current, updated := s.versions()
	if current == updated {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("no downloaded update is available"))
	}
	archivePath := s.platform.ResolvePath(appUpdateCacheFilePath)
	if !fileExists(archivePath) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("update cache file is missing"))
	}
	if !s.serviceMode && s.requestShutdown == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("application shutdown coordinator is unavailable"))
	}
	if err := ctx.Err(); err != nil {
		return nil, updateContextError(err)
	}
	preparedDir, err := prepareApplicationUpdate(archivePath)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("prepare application update: %w", err))
	}
	defer func() {
		if !handedOff {
			_ = os.RemoveAll(preparedDir)
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, updateContextError(err)
	}
	if err := launchUpdateHelper(ctx, archivePath, filepath.Base(preparedDir), s.serviceMode, s.logLevel, s.logDays); err != nil {
		if ctx.Err() != nil {
			return nil, updateContextError(ctx.Err())
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.mu.Lock()
	s.preparedUpdate = preparedDir
	s.mu.Unlock()
	handedOff = true
	if !s.serviceMode {
		s.requestShutdown()
	}
	return connect.NewResponse(&appv1.ApplyAppUpdateResponse{}), nil
}

// The helper removes its handoff directory when it fails to stop the service.
// Both apply and download can retry once this cleanup is positively confirmed.
// The caller must hold s.mu.
func (s *Service) releaseFailedHandoffLocked() {
	if s.applying && s.preparedUpdate != "" {
		if _, err := os.Lstat(s.preparedUpdate); errors.Is(err, os.ErrNotExist) {
			s.applying = false
			s.preparedUpdate = ""
		}
	}
}

func updateContextError(err error) error {
	code := connect.CodeCanceled
	if errors.Is(err, context.DeadlineExceeded) {
		code = connect.CodeDeadlineExceeded
	}
	return connect.NewError(code, err)
}

func (s *Service) versions() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentVersion, s.updatedVersion
}

func (s *Service) fetchLatestAsset(ctx context.Context) (githubRelease, githubAsset, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+Repository+"/releases/latest", nil)
	if err != nil {
		return githubRelease{}, githubAsset{}, err
	}
	options := s.requestNetworkOptions()
	if err := network.PrepareRequest(req, options, false); err != nil {
		return githubRelease{}, githubAsset{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client, err := newRequestClient(options, 20*time.Second, false)
	if err != nil {
		return githubRelease{}, githubAsset{}, err
	}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return githubRelease{}, githubAsset{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return githubRelease{}, githubAsset{}, fmt.Errorf("github release request failed: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return githubRelease{}, githubAsset{}, err
	}

	assetName := appUpdateAssetName()
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			return release, asset, nil
		}
	}
	return githubRelease{}, githubAsset{}, fmt.Errorf("asset not found: %s", assetName)
}

func (s *Service) downloadAsset(ctx context.Context, asset githubAsset, path string, progressEvent string) error {
	options := s.requestNetworkOptions()
	client, err := newRequestClient(options, 10*time.Minute, false)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	return platform.DownloadReleaseAssetWithClient(ctx, asset.BrowserDownloadURL, asset.Digest, path, options, client, func(downloaded, total int64) {
		s.publish(progressEvent, downloaded, total)
	})
}

func (s *Service) requestNetworkOptions() network.Options {
	if s.platform != nil {
		return s.platform.NetworkOptions()
	}
	if s.appConfig != nil {
		return s.appConfig.Current().NetworkOptions()
	}
	return network.Options{}
}

func (s *Service) publish(eventName string, data ...any) {
	if s.events != nil {
		s.events.Publish(eventName, data...)
	}
}

func appUpdateAssetName() string {
	return appUpdateAssetNameForPlatform(runtime.GOOS, runtime.GOARCH)
}

func appUpdateAssetNameForPlatform(goos, goarch string) string {
	archiveArch := goarch
	if goos == "linux" && goarch == "arm" {
		archiveArch = "armv7"
	}
	return fmt.Sprintf("%s-%s-%s.zip", appTitle, goos, archiveArch)
}

func normalizeVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

var launchUpdateHelper = startUpdateHelper

func startUpdateHelper(ctx context.Context, archivePath, preparedUpdate string, serviceMode bool, logLevel logging.Level, logDays int) (returnErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	currentExe, err := os.Executable()
	if err != nil {
		return err
	}
	preparedDir, err := preparedUpdateDirectory(archivePath, currentExe, preparedUpdate)
	if err != nil {
		return err
	}
	if err := validateUpdateCandidate(preparedDir); err != nil {
		return err
	}
	helperPath, err := copyUpdateHelper(currentExe)
	if err != nil {
		return err
	}
	defer func() {
		// Unix has already opened the executable when Start (or systemd's
		// Type=exec handoff) succeeds. Unlink our private copy now so repeated
		// router updates do not leave executable-sized files in RAM-backed /tmp.
		if returnErr != nil || runtime.GOOS != "windows" {
			_ = os.Remove(helperPath)
		}
	}()
	workingDir, _ := os.Getwd()
	restartArgs, err := json.Marshal(os.Args[1:])
	if err != nil {
		return err
	}

	identity, err := platform.IdentifyProcess(os.Getpid())
	if err != nil {
		return fmt.Errorf("identify update parent: %w", err)
	}
	args := updateHelperArguments(archivePath, preparedUpdate, currentExe, identity, string(restartArgs), workingDir, serviceMode, logLevel, logDays)
	if err := ctx.Err(); err != nil {
		return err
	}
	return startUpdateProcess(helperPath, args, workingDir, serviceMode)
}

func updateHelperArguments(archivePath, preparedUpdate, targetPath string, parent platform.ProcessIdentity, restartArgs, workingDir string, serviceMode bool, logLevel logging.Level, logDays int) []string {
	args := []string{
		appUpdateHelperCommand,
		"--archive-path", archivePath,
		"--prepared-update", preparedUpdate,
		"--target-path", targetPath,
		"--parent-pid", strconv.Itoa(parent.PID),
		"--parent-created", strconv.FormatInt(parent.Created, 10),
		"--parent-executable", parent.Executable,
		"--restart-args", restartArgs,
		"--working-dir", workingDir,
		"--log-level", logLevel.String(),
		"--log-days", strconv.Itoa(logDays),
	}
	if serviceMode {
		args = append(args, "--service-mode")
	}
	return args
}

func copyUpdateHelper(source string) (_ string, returnErr error) {
	in, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer in.Close()
	// Exclusive creation avoids following an attacker-created, predictable path
	// in a shared temporary directory. Keep this handle through the entire copy.
	out, err := os.CreateTemp("", appTitle+"-updater-*"+executableSuffix())
	if err != nil {
		return "", err
	}
	defer func() {
		_ = out.Close()
		if returnErr != nil {
			_ = os.Remove(out.Name())
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return "", err
	}
	if err := out.Chmod(0o755); err != nil {
		return "", err
	}
	if err := out.Sync(); err != nil {
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return out.Name(), nil
}

func executableSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
