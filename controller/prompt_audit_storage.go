package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

func GetPromptAuditStorage(c *gin.Context) {
	data, err := model.GetPromptAuditStorageStats(model.DB)
	respondPromptAuditRead(c, data, err)
}

func ListPromptAuditArchives(c *gin.Context) {
	archives, err := model.ListPromptAuditArchives(model.DB)
	items := make([]gin.H, 0, len(archives))
	for _, archive := range archives {
		var volumes []model.PromptAuditArchiveVolume
		_ = common.UnmarshalJsonStr(string(archive.Volumes), &volumes)
		items = append(items, gin.H{"archive": archive, "volumes": volumes})
	}
	respondPromptAuditRead(c, items, err)
}

func ListPromptAuditImports(c *gin.Context) {
	data, err := model.ListPromptAuditArchiveImports(model.DB)
	respondPromptAuditRead(c, data, err)
}

func ListPromptAuditImportedEvents(c *gin.Context) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 || page > 100000 {
		common.ApiError(c, errors.New("invalid page"))
		return
	}
	size, err := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if err != nil || size < 1 || size > 200 {
		common.ApiError(c, errors.New("invalid page size"))
		return
	}
	anchor, err := strconv.ParseInt(c.DefaultQuery("anchor", "0"), 10, 64)
	if err != nil || anchor < 0 {
		common.ApiError(c, errors.New("invalid anchor"))
		return
	}
	items, total, err := model.ListPromptAuditImportedEvents(model.DB, c.Param("source"), anchor, page, size)
	respondPromptAuditRead(c, gin.H{"items": items, "total": total, "page": page, "page_size": size}, err)
}

func GetPromptAuditImportedEvent(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		common.ApiError(c, errors.New("invalid event id"))
		return
	}
	full := authz.Can(c.GetInt("id"), c.GetInt("role"), authz.PromptAuditViewFullPrompt)
	data, err := model.GetPromptAuditImportedEvent(model.DB, c.Param("source"), id, full)
	if err == nil && full {
		model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), "Viewed imported prompt audit content", c.ClientIP(), "prompt_audit.view_full_prompt", map[string]any{"source_id": c.Param("source"), "prompt_audit_id": id}, auditOperatorInfo(c), &model.AuditRequestInfo{Method: http.MethodGet, Route: c.FullPath(), Path: c.Request.URL.Path, Status: http.StatusOK, Success: true}, c)
	}
	respondPromptAuditRead(c, data, err)
}

type promptAuditImportFile struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}
type promptAuditImportJob struct {
	ID        string                  `json:"id"`
	Status    string                  `json:"status"`
	CreatedAt int64                   `json:"created_at"`
	Files     []promptAuditImportFile `json:"files"`
	Message   string                  `json:"message,omitempty"`
}

var promptAuditImportName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,199}$`)

func GetPromptAuditImportJob(c *gin.Context) {
	if !promptAuditImportName.MatchString(c.Param("id")) {
		common.ApiError(c, errors.New("invalid import job"))
		return
	}
	root := os.Getenv("PROMPT_AUDIT_IMPORT_DIR")
	if root == "" {
		common.ApiError(c, errors.New("encrypted archive importer is not configured"))
		return
	}
	path := filepath.Join(root, c.Param("id"), "status.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		data, err = os.ReadFile(filepath.Join(root, c.Param("id"), "ready.json"))
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var job promptAuditImportJob
	if err := common.Unmarshal(data, &job); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, job)
}

func ImportPromptAuditArchive(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, model.PromptAuditImportCacheBytes+(16<<20))
	reader, err := c.Request.MultipartReader()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	root := os.Getenv("PROMPT_AUDIT_IMPORT_DIR")
	job := promptAuditImportJob{ID: common.GetUUID(), Status: "queued", CreatedAt: common.GetTimestamp()}
	var directory string
	queued := false
	locked := false
	defer func() {
		if directory != "" && !queued {
			_ = os.RemoveAll(directory)
		}
		if locked {
			_ = os.Remove(filepath.Join(root, ".uploading"))
		}
	}()
	var total int64
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			common.ApiError(c, err)
			return
		}
		name := part.FileName()
		if part.FormName() != "files" || !promptAuditImportName.MatchString(name) {
			common.ApiError(c, errors.New("invalid archive filename"))
			return
		}
		if strings.HasSuffix(name, ".tar.zst") && directory == "" {
			if _, err := model.PromptAuditStorageSource(model.DB); err != nil {
				common.ApiError(c, err)
				return
			}
			data, err := model.ImportPromptAuditArchive(model.DB.WithContext(c.Request.Context()), part, false)
			if err != nil {
				common.ApiError(c, err)
				return
			}
			common.ApiSuccess(c, gin.H{"status": "done", "import": data})
			return
		}
		if root == "" {
			common.ApiError(c, errors.New("encrypted archive importer is not configured"))
			return
		}
		if directory == "" {
			if err := os.MkdirAll(root, 0700); err != nil {
				common.ApiError(c, err)
				return
			}
			if err := os.Mkdir(filepath.Join(root, ".uploading"), 0700); err != nil {
				common.ApiError(c, errors.New("another archive upload is in progress"))
				return
			}
			locked = true
			// Pending uploads have their own bounded staging budget and 24h expiry.
			err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() {
					return nil
				}
				info, err := entry.Info()
				if err != nil {
					return err
				}
				total += info.Size()
				return nil
			})
			if err != nil {
				_ = os.Remove(filepath.Join(root, ".uploading"))
				common.ApiError(c, err)
				return
			}
			directory = filepath.Join(root, job.ID)
			if err := os.Mkdir(directory, 0700); err != nil {
				common.ApiError(c, err)
				return
			}
		}
		file, err := os.OpenFile(filepath.Join(directory, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			common.ApiError(c, errors.New("duplicate archive volume"))
			return
		}
		digest := sha256.New()
		count, copyErr := io.Copy(io.MultiWriter(file, digest), io.LimitReader(part, max(model.PromptAuditImportCacheBytes-total, 0)+1))
		closeErr := file.Close()
		total += count
		if copyErr != nil || closeErr != nil || total > model.PromptAuditImportCacheBytes {
			common.ApiError(c, errors.New("archive upload staging exceeds 2 GiB"))
			return
		}
		job.Files = append(job.Files, promptAuditImportFile{Name: name, Bytes: count, SHA256: hex.EncodeToString(digest.Sum(nil))})
		if len(job.Files) > 128 {
			common.ApiError(c, errors.New("too many archive volumes"))
			return
		}
	}
	if len(job.Files) == 0 {
		common.ApiError(c, errors.New("archive files are required"))
		return
	}
	data, err := common.Marshal(job)
	if err == nil {
		err = os.WriteFile(filepath.Join(directory, "ready.json.tmp"), data, 0600)
		if err == nil {
			err = os.Rename(filepath.Join(directory, "ready.json.tmp"), filepath.Join(directory, "ready.json"))
		}
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	queued = true
	common.ApiSuccess(c, job)
}
