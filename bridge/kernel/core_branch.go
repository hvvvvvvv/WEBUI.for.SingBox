package kernel

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"guiforcores/bridge/logging"
	"guiforcores/bridge/network"
	"guiforcores/bridge/platform"
	"guiforcores/bridge/rpcutil"
	appv1 "guiforcores/gen/app/v1"
	kernelv1 "guiforcores/gen/kernel/v1"

	connect "connectrpc.com/connect"
)

const (
	coreCacheFilePath       = coreWorkingDirectory + "/cache.db"
	stableReleaseAPIURL     = "https://api.github.com/repos/SagerNet/sing-box/releases/latest"
	alphaReleaseAPIURL      = "https://api.github.com/repos/SagerNet/sing-box/releases?per_page=3"
	stableReleasePageURL    = "https://github.com/SagerNet/sing-box/releases/latest"
	alphaReleasePageURL     = "https://github.com/SagerNet/sing-box/releases"
	coreDownloadCacheFolder = "data/.cache"
)

var coreVersionRE = regexp.MustCompile(`version\s+(\S+)`)

type githubRelease struct {
	TagName    string        `json:"tag_name"`
	Prerelease bool          `json:"prerelease"`
	Assets     []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
	Uploader           struct {
		Type string `json:"type"`
	} `json:"uploader"`
}

func (s *Service) GetCoreBranchLocalVersion(
	_ context.Context,
	req *connect.Request[kernelv1.GetCoreBranchLocalVersionRequest],
) (*connect.Response[kernelv1.GetCoreBranchLocalVersionResponse], error) {
	info := s.coreBranchLocalVersion(req.Msg.GetBranch())
	return connect.NewResponse(info), nil
}

func (s *Service) GetCoreBranchRemoteVersion(
	ctx context.Context,
	req *connect.Request[kernelv1.GetCoreBranchRemoteVersionRequest],
) (response *connect.Response[kernelv1.GetCoreBranchRemoteVersionResponse], responseErr error) {
	started := time.Now()
	branch := req.Msg.GetBranch()
	defer func() {
		version := ""
		if response != nil {
			version = response.Msg.GetRemoteVersion()
		}
		logging.Complete(ctx, "kernel", "check_update", "core update checked", started, responseErr, "branch", branch.String(), "remote_version", version)
	}()
	release, asset, assetName, err := s.fetchCoreReleaseAsset(ctx, branch)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&kernelv1.GetCoreBranchRemoteVersionResponse{
		RemoteVersion:  strings.TrimPrefix(release.TagName, "v"),
		AssetName:      assetName,
		ReleasePageUrl: coreReleasePageURL(branch),
		TrustedAsset:   asset.Uploader.Type == "Bot",
	}), nil
}

func (s *Service) DownloadCore(
	ctx context.Context,
	req *connect.Request[kernelv1.DownloadCoreRequest],
) (response *connect.Response[kernelv1.DownloadCoreResponse], responseErr error) {
	started := time.Now()
	branch := req.Msg.GetBranch()
	progressEvent := strings.TrimSpace(req.Msg.GetProgressEvent())
	defer func() {
		logging.Complete(ctx, "kernel", "download", "core downloaded", started, responseErr, "branch", branch.String(), "progress_event", progressEvent)
	}()
	if progressEvent == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("progress_event is required"))
	}
	downloadCtx, cancel := context.WithCancel(ctx)
	stopCancellation := context.AfterFunc(s.lifecycleCtx, cancel)
	s.mu.Lock()
	if s.closing || len(s.downloads) != 0 {
		closing := s.closing
		s.mu.Unlock()
		stopCancellation()
		cancel()
		if closing {
			return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("application is shutting down"))
		}
		return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("a core download is already in progress"))
	}
	if s.coreBranchInUseLocked(branch) {
		s.mu.Unlock()
		stopCancellation()
		cancel()
		return nil, coreUpdateRequiresStopError()
	}
	s.downloads[progressEvent] = cancel
	s.operations.Add(1)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.downloads, progressEvent)
		s.mu.Unlock()
		stopCancellation()
		cancel()
		s.operations.Done()
	}()

	_, asset, assetName, err := s.fetchCoreReleaseAsset(downloadCtx, branch)
	if err != nil {
		if downloadCtx.Err() != nil {
			return nil, rpcutil.AsConnectError(downloadCtx.Err())
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if asset.Uploader.Type != "Bot" && !req.Msg.GetAllowUntrustedAsset() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("asset %s was not uploaded by Bot", assetName))
	}

	coreDir := s.processes.ResolvePath(coreWorkingDirectory)
	cacheDir := s.processes.ResolvePath(coreDownloadCacheFolder)
	downloadCacheFile := filepath.Join(cacheDir, assetName)
	if err := os.MkdirAll(coreDir, 0o755); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	if err := s.downloadCoreAsset(downloadCtx, asset, downloadCacheFile, progressEvent); err != nil {
		if errors.Is(downloadCtx.Err(), context.Canceled) {
			return nil, connect.NewError(connect.CodeCanceled, fmt.Errorf("download canceled"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer os.Remove(downloadCacheFile)

	if err := s.installCoreArchive(downloadCtx, downloadCacheFile, cacheDir, branch); err != nil {
		return nil, rpcutil.AsConnectError(err)
	}

	info := s.coreBranchLocalVersion(branch)
	return connect.NewResponse(&kernelv1.DownloadCoreResponse{
		LocalVersion:  info.GetLocalVersion(),
		VersionDetail: info.GetVersionDetail(),
		Rollbackable:  info.GetRollbackable(),
	}), nil
}

func (s *Service) CancelCoreDownload(
	ctx context.Context,
	req *connect.Request[kernelv1.CancelCoreDownloadRequest],
) (*connect.Response[kernelv1.CancelCoreDownloadResponse], error) {
	progressEvent := strings.TrimSpace(req.Msg.GetProgressEvent())
	if progressEvent == "" {
		return connect.NewResponse(&kernelv1.CancelCoreDownloadResponse{}), nil
	}

	s.mu.Lock()
	cancel := s.downloads[progressEvent]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	logging.FromContext(ctx).InfoContext(ctx, "core download cancelled", "component", "kernel", "operation", "cancel_download", "progress_event", progressEvent, "result", "cancelled")
	return connect.NewResponse(&kernelv1.CancelCoreDownloadResponse{}), nil
}

func (s *Service) RollbackCore(
	ctx context.Context,
	req *connect.Request[kernelv1.RollbackCoreRequest],
) (response *connect.Response[kernelv1.RollbackCoreResponse], responseErr error) {
	started := time.Now()
	branch := req.Msg.GetBranch()
	defer func() {
		logging.Complete(ctx, "kernel", "rollback", "core rolled back", started, responseErr, "branch", branch.String())
	}()
	candidatePath := coreFilePathForBranch(branch) + ".bak"
	if err := s.checkCoreVersion(ctx, candidatePath, nil); err != nil {
		return nil, err
	}
	action := func() error {
		corePath := s.processes.ResolvePath(coreFilePathForBranch(branch))
		if _, err := os.Stat(corePath); errors.Is(err, os.ErrNotExist) {
			return os.Rename(corePath+".bak", corePath)
		}
		return swapCoreFiles(corePath, corePath+".bak")
	}
	if err := s.runWithCoreRestartIfCurrent(ctx, branch, candidatePath, action, action); err != nil {
		return nil, rpcutil.AsConnectError(err)
	}

	info := s.coreBranchLocalVersion(branch)
	return connect.NewResponse(&kernelv1.RollbackCoreResponse{
		LocalVersion:  info.GetLocalVersion(),
		VersionDetail: info.GetVersionDetail(),
		Rollbackable:  info.GetRollbackable(),
	}), nil
}

func (s *Service) ClearCoreCache(
	ctx context.Context,
	req *connect.Request[kernelv1.ClearCoreCacheRequest],
) (response *connect.Response[kernelv1.ClearCoreCacheResponse], responseErr error) {
	started := time.Now()
	branch := req.Msg.GetBranch()
	defer func() {
		logging.Complete(ctx, "kernel", "clear_cache", "core cache cleared", started, responseErr, "branch", branch.String())
	}()
	action := func() error {
		err := os.Remove(s.processes.ResolvePath(coreCacheFilePath))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := s.clearSharedCoreCache(ctx, action); err != nil {
		return nil, rpcutil.AsConnectError(err)
	}
	return connect.NewResponse(&kernelv1.ClearCoreCacheResponse{}), nil
}

func (s *Service) coreBranchLocalVersion(branch appv1.KernelBranch) *kernelv1.GetCoreBranchLocalVersionResponse {
	corePath := coreFilePathForBranch(branch)
	rollbackable := fileExists(s.processes.ResolvePath(corePath + ".bak"))
	result := s.processes.Exec(corePath, []string{"version"}, platform.ExecOptions{})
	if !result.Flag {
		return &kernelv1.GetCoreBranchLocalVersionResponse{Rollbackable: rollbackable}
	}

	version := ""
	if matches := coreVersionRE.FindStringSubmatch(result.Data); len(matches) > 1 {
		version = matches[1]
	}
	return &kernelv1.GetCoreBranchLocalVersionResponse{
		LocalVersion:  version,
		VersionDetail: strings.TrimSpace(result.Data),
		Rollbackable:  rollbackable,
	}
}

func (s *Service) fetchCoreReleaseAsset(ctx context.Context, branch appv1.KernelBranch) (githubRelease, githubAsset, string, error) {
	release, err := s.fetchCoreRelease(ctx, branch)
	if err != nil {
		return githubRelease{}, githubAsset{}, "", err
	}

	version := strings.TrimPrefix(release.TagName, "v")
	assetName := getKernelAssetFileName(version)
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			return release, asset, assetName, nil
		}
	}
	return githubRelease{}, githubAsset{}, assetName, fmt.Errorf("asset not found: %s", assetName)
}

func (s *Service) fetchCoreRelease(ctx context.Context, branch appv1.KernelBranch) (githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, coreReleaseAPIURL(branch), nil)
	if err != nil {
		return githubRelease{}, err
	}
	options := s.requestNetworkOptions()
	if err := network.PrepareRequest(req, options, false); err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client, err := newRequestClient(options, 20*time.Second, false)
	if err != nil {
		return githubRelease{}, err
	}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return githubRelease{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return githubRelease{}, fmt.Errorf("github release request failed: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}

	if branchIsAlpha(branch) {
		var releases []githubRelease
		if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
			return githubRelease{}, err
		}
		for _, release := range releases {
			if release.Prerelease {
				return release, nil
			}
		}
		return githubRelease{}, fmt.Errorf("alpha release not found")
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return githubRelease{}, err
	}
	return release, nil
}

func (s *Service) downloadCoreAsset(ctx context.Context, asset githubAsset, path string, progressEvent string) error {
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

var newRequestClient = network.NewClient

func (s *Service) requestNetworkOptions() network.Options {
	options := network.Options{}
	if s.appConfig != nil {
		options = s.appConfig.Current().NetworkOptions()
	}
	options.CoreProxy = s.CoreRequestProxy
	return options
}

func (s *Service) installCoreArchive(ctx context.Context, archivePath string, cacheDir string, branch appv1.KernelBranch) error {
	extractDir, err := os.MkdirTemp(cacheDir, "core-extract-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(extractDir)
	extractName := filepath.Base(archivePath)
	if strings.HasSuffix(extractName, ".tar.gz") {
		extractName = strings.TrimSuffix(extractName, ".tar.gz")
		if err := extractTarGZ(archivePath, extractDir); err != nil {
			return err
		}
	} else if strings.HasSuffix(extractName, ".zip") {
		extractName = strings.TrimSuffix(extractName, ".zip")
		if err := extractZip(archivePath, extractDir); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("unsupported core archive: %s", archivePath)
	}

	sourcePath := filepath.Join(extractDir, extractName, getKernelFileName(false))
	if runtime.GOOS != "windows" {
		if err := os.Chmod(sourcePath, 0o755); err != nil {
			return err
		}
	}
	if err := s.checkCoreVersion(ctx, sourcePath, nil); err != nil {
		return err
	}
	// Starting/stopping the core and replacing its executable must not overlap.
	// Renaming a live Linux binary changes /proc/PID/exe, invalidating the guard's
	// recorded identity. Installation never restarts a user's network implicitly.
	s.coreTransitionMu.Lock()
	defer s.coreTransitionMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	closing, inUse := s.closing, s.coreBranchInUseLocked(branch)
	s.mu.Unlock()
	if closing {
		return connect.NewError(connect.CodeUnavailable, fmt.Errorf("application is shutting down"))
	}
	if inUse {
		return coreUpdateRequiresStopError()
	}
	targetPath := s.processes.ResolvePath(coreFilePathForBranch(branch))
	backupPath := targetPath + ".bak"
	if err := os.Remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if fileExists(targetPath) {
		if err := os.Rename(targetPath, backupPath); err != nil {
			return err
		}
	}
	if err := os.Rename(sourcePath, targetPath); err != nil {
		if fileExists(backupPath) {
			return errors.Join(err, os.Rename(backupPath, targetPath))
		}
		return err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(targetPath, 0o755)
	}
	return nil
}

func coreUpdateRequiresStopError() error {
	return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("stop the running core before updating this branch"))
}

// Must be called with s.mu held. Prefer the actual started instance over the
// currently selected app settings, which can change before an automatic restart.
func (s *Service) coreBranchInUseLocked(branch appv1.KernelBranch) bool {
	if s.corePID <= 0 && s.status != kernelv1.CoreStatus_CORE_STATUS_STARTING && s.status != kernelv1.CoreStatus_CORE_STATUS_STOPPING {
		return false
	}
	if s.currentStart != nil {
		return (s.currentStart.runtime.Branch == "alpha") == branchIsAlpha(branch)
	}
	// A failed start can retain a live PID while its successful-start snapshot
	// is absent. Mutable app settings cannot establish which executable it owns.
	// Block both branches until that process has been confirmed stopped.
	return true
}

func (s *Service) runWithCoreRestartIfCurrent(ctx context.Context, branch appv1.KernelBranch, candidatePath string, action func() error, undo ...func() error) error {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return connect.NewError(connect.CodeUnavailable, fmt.Errorf("application is shutting down"))
	}
	s.operations.Add(1)
	generation := s.stopGeneration
	s.mu.Unlock()
	defer s.operations.Done()
	ctx, cancel := context.WithCancel(ctx)
	stopCancellation := context.AfterFunc(s.lifecycleCtx, cancel)
	defer func() { stopCancellation(); cancel() }()

	s.restartOperationMu.Lock()
	defer s.restartOperationMu.Unlock()
	ctx, recoveryCtx, finish, err := s.beginCoreTransition(ctx, generation)
	if err != nil {
		return err
	}
	defer finish()
	s.mu.Lock()
	inUse, status, previous := s.coreBranchInUseLocked(branch), s.status, s.currentStart
	pending := s.restartRequired
	s.mu.Unlock()
	if !inUse {
		return action()
	}
	if status != kernelv1.CoreStatus_CORE_STATUS_RUNNING || previous == nil {
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("stop the core before changing its executable"))
	}
	prepared, err := previous.prepared()
	if err != nil {
		return err
	}
	version, err := s.readCompatibleCoreVersion(ctx, candidatePath, prepared.runtime.Env)
	if err != nil {
		return err
	}
	adaptConfigToCoreVersion(prepared.config, version)
	if err := s.checkPreparedConfig(ctx, candidatePath, prepared.runtime.Env, prepared.config); err != nil {
		return err
	}
	if _, err := s.stopCore(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return s.restorePreviousCore(recoveryCtx, generation, previous, rpcutil.AsConnectError(err))
	}
	if err := action(); err != nil {
		if errors.Is(err, errCoreFileRestoreUnconfirmed) {
			return err
		}
		return s.restorePreviousCore(recoveryCtx, generation, previous, err)
	}
	prepared.path = coreFilePathForBranch(branch)
	if err = s.setStarting(previous.profile.GetId(), generation); err == nil {
		_, err = s.launchPreparedCore(ctx, previous.profile, prepared)
	}
	if err == nil {
		if pending {
			s.updateCoreState(func() { s.restartRequired = true })
		}
		return nil
	}
	s.mu.Lock()
	unsafe := s.corePID > 0
	s.mu.Unlock()
	if !unsafe && !errors.Is(err, platform.ErrCoreExitUnconfirmed) && len(undo) > 0 {
		if undoErr := undo[0](); undoErr != nil {
			return restartRecoveryError(err, "previous executable restore failed; core was not restored", undoErr)
		}
	}
	return s.restorePreviousCore(recoveryCtx, generation, previous, err)
}

func extractZip(archivePath string, targetDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, file := range reader.File {
		path, err := safeJoin(targetDir, file.Name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(path, file.Mode()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		if err := writeExtractedFile(path, src, file.Mode()); err != nil {
			_ = src.Close()
			return err
		}
		if err := src.Close(); err != nil {
			return err
		}
	}
	return nil
}

func extractTarGZ(archivePath string, targetDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		path, err := safeJoin(targetDir, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := writeExtractedFile(path, reader, os.FileMode(header.Mode)); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeExtractedFile(path string, reader io.Reader, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, reader)
	return err
}

func safeJoin(baseDir string, name string) (string, error) {
	target := filepath.Join(baseDir, name)
	cleanBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}
	cleanTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if cleanTarget != cleanBase && !strings.HasPrefix(cleanTarget, cleanBase+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe archive path: %s", name)
	}
	return target, nil
}

func coreReleaseAPIURL(branch appv1.KernelBranch) string {
	if branchIsAlpha(branch) {
		return alphaReleaseAPIURL
	}
	return stableReleaseAPIURL
}

func coreReleasePageURL(branch appv1.KernelBranch) string {
	if branchIsAlpha(branch) {
		return alphaReleasePageURL
	}
	return stableReleasePageURL
}

func coreFilePathForBranch(branch appv1.KernelBranch) string {
	return coreWorkingDirectory + "/" + getKernelFileName(branchIsAlpha(branch))
}

func branchIsAlpha(branch appv1.KernelBranch) bool {
	return branch == appv1.KernelBranch_KERNEL_BRANCH_ALPHA
}

func branchConfigValue(branch appv1.KernelBranch) string {
	if branchIsAlpha(branch) {
		return "alpha"
	}
	return "main"
}

func getKernelAssetFileName(version string) string {
	libc := ""
	if runtime.GOOS == "linux" {
		libc = platform.DetectLibc()
	}
	return getKernelAssetFileNameForPlatform(version, runtime.GOOS, runtime.GOARCH, libc)
}

func getKernelAssetFileNameForPlatform(version, goos, goarch, libc string) string {
	suffix := ".tar.gz"
	if goos == "windows" {
		suffix = ".zip"
	}
	libcSuffix := ""
	if goos == "linux" {
		// The application's ARM build uses GOARM=7; upstream names it armv7.
		switch goarch {
		case "arm":
			goarch = "armv7"
		case "mips", "mipsle":
			// Use the portable static soft-float build on routers, including
			// devices without an FPU or with musl/uClibc instead of glibc.
			goarch += "-softfloat"
			libc = ""
		}
		if libc := strings.TrimSpace(libc); libc != "" {
			libcSuffix = "-" + libc
		}
	}
	return fmt.Sprintf("sing-box-%s-%s-%s%s%s", version, goos, goarch, libcSuffix, suffix)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
