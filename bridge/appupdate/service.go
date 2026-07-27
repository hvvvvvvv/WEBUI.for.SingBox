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
	"strings"
	"sync"
	"time"

	"guiforcores/bridge/config"
	"guiforcores/bridge/platform"
	appv1 "guiforcores/gen/app/v1"

	connect "connectrpc.com/connect"
)

const (
	appTitle               = "webui.for.singbox"
	appLatestReleaseAPIURL = "https://api.github.com/repos/hvvvvvvv/WEBUI.for.SingBox/releases/latest"
	appUpdateCacheFilePath = "data/.cache/gui-update.zip"
	appUpdateHelperCommand = "__updater"
)

type AppConfigReader interface {
	Current() config.AppConfig
}

type EventPublisher interface {
	Publish(eventName string, data ...any)
}

type Service struct {
	platform       *platform.Service
	appConfig      AppConfigReader
	events         EventPublisher
	currentVersion string
	serviceMode    bool

	mu             sync.Mutex
	updatedVersion string
	downloads      map[string]context.CancelFunc
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func NewService(
	platformService *platform.Service,
	appConfig AppConfigReader,
	events EventPublisher,
	currentVersion string,
	serviceModes ...bool,
) *Service {
	currentVersion = strings.TrimSpace(currentVersion)
	if currentVersion == "" {
		currentVersion = "unknown"
	}
	serviceMode := len(serviceModes) > 0 && serviceModes[0]
	return &Service{
		platform:       platformService,
		appConfig:      appConfig,
		events:         events,
		currentVersion: currentVersion,
		serviceMode:    serviceMode,
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
) (*connect.Response[appv1.CheckAppUpdateResponse], error) {
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
) (*connect.Response[appv1.DownloadAppUpdateResponse], error) {
	progressEvent := strings.TrimSpace(req.Msg.GetProgressEvent())
	if progressEvent == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("progress_event is required"))
	}

	release, asset, err := s.fetchLatestAsset(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	downloadCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.downloads[progressEvent] = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.downloads, progressEvent)
		s.mu.Unlock()
		cancel()
	}()

	cachePath := s.platform.ResolvePath(appUpdateCacheFilePath)
	_ = os.Remove(cachePath)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := s.downloadAsset(downloadCtx, asset.BrowserDownloadURL, cachePath, progressEvent); err != nil {
		_ = os.Remove(cachePath)
		if errors.Is(downloadCtx.Err(), context.Canceled) {
			return nil, connect.NewError(connect.CodeCanceled, fmt.Errorf("download canceled"))
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
	_ context.Context,
	req *connect.Request[appv1.CancelAppUpdateRequest],
) (*connect.Response[appv1.CancelAppUpdateResponse], error) {
	progressEvent := strings.TrimSpace(req.Msg.GetProgressEvent())
	if progressEvent != "" {
		s.mu.Lock()
		cancel := s.downloads[progressEvent]
		delete(s.downloads, progressEvent)
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
	_ = os.Remove(s.platform.ResolvePath(appUpdateCacheFilePath))
	return connect.NewResponse(&appv1.CancelAppUpdateResponse{}), nil
}

func (s *Service) ApplyAppUpdate(
	_ context.Context,
	_ *connect.Request[appv1.ApplyAppUpdateRequest],
) (*connect.Response[appv1.ApplyAppUpdateResponse], error) {
	current, updated := s.versions()
	if current == updated {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("no downloaded update is available"))
	}
	archivePath := s.platform.ResolvePath(appUpdateCacheFilePath)
	if !fileExists(archivePath) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("update cache file is missing"))
	}
	if err := startUpdateHelper(archivePath, s.serviceMode); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !s.serviceMode {
		go func() {
			time.Sleep(300 * time.Millisecond)
			os.Exit(0)
		}()
	}
	return connect.NewResponse(&appv1.ApplyAppUpdateResponse{}), nil
}

func (s *Service) versions() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentVersion, s.updatedVersion
}

func (s *Service) fetchLatestAsset(ctx context.Context) (githubRelease, githubAsset, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, appLatestReleaseAPIURL, nil)
	if err != nil {
		return githubRelease{}, githubAsset{}, err
	}
	if s.appConfig != nil {
		if token := strings.TrimSpace(s.appConfig.Current().GitHubAPIToken); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
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

func (s *Service) downloadAsset(ctx context.Context, url string, path string, progressEvent string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if s.appConfig != nil {
		if token := strings.TrimSpace(s.appConfig.Current().GitHubAPIToken); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("download failed: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	buffer := make([]byte, 128*1024)
	var downloaded int64
	total := resp.ContentLength
	for {
		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			if _, err := file.Write(buffer[:n]); err != nil {
				return err
			}
			downloaded += int64(n)
			s.publish(progressEvent, downloaded, total)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return nil
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

func startUpdateHelper(archivePath string, serviceMode bool) error {
	currentExe, err := os.Executable()
	if err != nil {
		return err
	}
	helperPath := filepath.Join(os.TempDir(), fmt.Sprintf("%s-updater-%d%s", appTitle, os.Getpid(), executableSuffix()))
	if err := copyFile(currentExe, helperPath, 0o755); err != nil {
		return err
	}
	workingDir, _ := os.Getwd()
	restartArgs, err := json.Marshal(os.Args[1:])
	if err != nil {
		return err
	}

	args := updateHelperArguments(archivePath, currentExe, os.Getpid(), string(restartArgs), workingDir, serviceMode)
	return startUpdateProcess(helperPath, args, workingDir, serviceMode)
}

func updateHelperArguments(archivePath, targetPath string, parentPID int, restartArgs, workingDir string, serviceMode bool) []string {
	args := []string{
		appUpdateHelperCommand,
		"--archive-path", archivePath,
		"--target-path", targetPath,
		"--parent-pid", fmt.Sprintf("%d", parentPID),
		"--restart-args", restartArgs,
		"--working-dir", workingDir,
	}
	if serviceMode {
		args = append(args, "--service-mode")
	}
	return args
}

func copyFile(source string, target string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Chmod(mode)
}

func executableSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
