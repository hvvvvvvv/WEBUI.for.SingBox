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

	"guiforcores/bridge/platform"
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
) (*connect.Response[kernelv1.GetCoreBranchRemoteVersionResponse], error) {
	release, asset, assetName, err := s.fetchCoreReleaseAsset(ctx, req.Msg.GetBranch())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&kernelv1.GetCoreBranchRemoteVersionResponse{
		RemoteVersion:  strings.TrimPrefix(release.TagName, "v"),
		AssetName:      assetName,
		ReleasePageUrl: coreReleasePageURL(req.Msg.GetBranch()),
		TrustedAsset:   asset.Uploader.Type == "Bot",
	}), nil
}

func (s *Service) DownloadCore(
	ctx context.Context,
	req *connect.Request[kernelv1.DownloadCoreRequest],
) (*connect.Response[kernelv1.DownloadCoreResponse], error) {
	branch := req.Msg.GetBranch()
	progressEvent := strings.TrimSpace(req.Msg.GetProgressEvent())
	if progressEvent == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("progress_event is required"))
	}

	_, asset, assetName, err := s.fetchCoreReleaseAsset(ctx, branch)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if asset.Uploader.Type != "Bot" && !req.Msg.GetAllowUntrustedAsset() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("asset %s was not uploaded by Bot", assetName))
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

	coreDir := s.processes.ResolvePath(coreWorkingDirectory)
	cacheDir := s.processes.ResolvePath(coreDownloadCacheFolder)
	downloadCacheFile := filepath.Join(cacheDir, assetName)
	if err := os.MkdirAll(coreDir, 0o755); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	if err := s.downloadCoreAsset(downloadCtx, asset.BrowserDownloadURL, downloadCacheFile, progressEvent); err != nil {
		if errors.Is(downloadCtx.Err(), context.Canceled) {
			_ = os.Remove(downloadCacheFile)
			return nil, connect.NewError(connect.CodeCanceled, fmt.Errorf("download canceled"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	if err := s.installCoreArchive(downloadCacheFile, cacheDir, branch); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	_ = os.Remove(downloadCacheFile)

	info := s.coreBranchLocalVersion(branch)
	return connect.NewResponse(&kernelv1.DownloadCoreResponse{
		LocalVersion:  info.GetLocalVersion(),
		VersionDetail: info.GetVersionDetail(),
		Rollbackable:  info.GetRollbackable(),
	}), nil
}

func (s *Service) CancelCoreDownload(
	_ context.Context,
	req *connect.Request[kernelv1.CancelCoreDownloadRequest],
) (*connect.Response[kernelv1.CancelCoreDownloadResponse], error) {
	progressEvent := strings.TrimSpace(req.Msg.GetProgressEvent())
	if progressEvent == "" {
		return connect.NewResponse(&kernelv1.CancelCoreDownloadResponse{}), nil
	}

	s.mu.Lock()
	cancel := s.downloads[progressEvent]
	delete(s.downloads, progressEvent)
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return connect.NewResponse(&kernelv1.CancelCoreDownloadResponse{}), nil
}

func (s *Service) RollbackCore(
	ctx context.Context,
	req *connect.Request[kernelv1.RollbackCoreRequest],
) (*connect.Response[kernelv1.RollbackCoreResponse], error) {
	branch := req.Msg.GetBranch()
	action := func() error {
		corePath := s.processes.ResolvePath(coreFilePathForBranch(branch))
		bakPath := corePath + ".bak"
		if _, err := os.Stat(bakPath); err != nil {
			return err
		}
		_ = os.Remove(corePath)
		return os.Rename(bakPath, corePath)
	}
	if err := s.runWithCoreRestartIfCurrent(ctx, branch, action); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
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
) (*connect.Response[kernelv1.ClearCoreCacheResponse], error) {
	action := func() error {
		err := os.Remove(s.processes.ResolvePath(coreCacheFilePath))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := s.runWithCoreRestartIfCurrent(ctx, req.Msg.GetBranch(), action); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
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
	if token := strings.TrimSpace(s.appConfig.Current().GitHubAPIToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
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

func (s *Service) downloadCoreAsset(ctx context.Context, url string, path string, progressEvent string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if token := strings.TrimSpace(s.appConfig.Current().GitHubAPIToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := (&http.Client{Timeout: 0}).Do(req)
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

func (s *Service) installCoreArchive(archivePath string, cacheDir string, branch appv1.KernelBranch) error {
	extractName := filepath.Base(archivePath)
	if strings.HasSuffix(extractName, ".tar.gz") {
		extractName = strings.TrimSuffix(extractName, ".tar.gz")
		if err := extractTarGZ(archivePath, cacheDir); err != nil {
			return err
		}
	} else if strings.HasSuffix(extractName, ".zip") {
		extractName = strings.TrimSuffix(extractName, ".zip")
		if err := extractZip(archivePath, cacheDir); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("unsupported core archive: %s", archivePath)
	}
	defer os.RemoveAll(filepath.Join(cacheDir, extractName))

	sourcePath := filepath.Join(cacheDir, extractName, getKernelFileName(false))
	targetPath := s.processes.ResolvePath(coreFilePathForBranch(branch))
	backupPath := targetPath + ".bak"
	_ = os.Remove(backupPath)
	if fileExists(targetPath) {
		_ = os.Rename(targetPath, backupPath)
	}
	_ = os.Remove(targetPath)
	if err := os.Rename(sourcePath, targetPath); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(targetPath, 0o755)
	}
	return nil
}

func (s *Service) runWithCoreRestartIfCurrent(ctx context.Context, branch appv1.KernelBranch, action func() error) error {
	s.mu.Lock()
	status := s.status
	activeProfileID := s.activeProfileID
	isCurrentBranch := s.appConfig.Current().Branch == branchConfigValue(branch)
	s.mu.Unlock()

	if status != kernelv1.CoreStatus_CORE_STATUS_RUNNING || !isCurrentBranch {
		return action()
	}

	if _, err := s.StopCore(ctx, connect.NewRequest(&kernelv1.StopCoreRequest{})); err != nil {
		return err
	}
	if err := action(); err != nil {
		return err
	}
	if activeProfileID == "" {
		return fmt.Errorf("profile_id is required for restart when no active profile exists")
	}
	_, err := s.StartCore(ctx, connect.NewRequest(&kernelv1.StartCoreRequest{ProfileId: activeProfileID}))
	return err
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
	suffix := ".tar.gz"
	if runtime.GOOS == "windows" {
		suffix = ".zip"
	}
	libcSuffix := ""
	if runtime.GOOS == "linux" {
		if libc := strings.TrimSpace(platform.DetectLibc()); libc != "" {
			libcSuffix = "-" + libc
		}
	}
	return fmt.Sprintf("sing-box-%s-%s-%s%s%s", version, runtime.GOOS, runtime.GOARCH, libcSuffix, suffix)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
