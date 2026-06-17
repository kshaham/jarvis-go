package handler

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"

	"github.com/kshaham/jarvis/internal/office"
	"github.com/kshaham/jarvis/internal/web/request"
)

// maxCodebaseUpload bounds the compressed upload at 500 MB. The
// uncompressed footprint is separately bounded inside office.extractZip
// (maxExtractedBytes / maxExtractedFiles) so a small zip-bomb can't fill
// the disk.
const maxCodebaseUpload = int64(500) << 20

// OfficeUploadCodebase receives a multipart upload of a .zip codebase and
// extracts it into a fresh /app/workspace/<projectName> so a team can be
// spawned with that code as its starting point. It streams the upload to
// a temp file (never buffering 500 MB in memory) and hands extraction to
// office.PrepareZipProject, which hardens against zip-slip / symlink /
// zip-bomb attacks.
//
// Request: multipart/form-data with a "projectName" text field and a
// "file" part carrying the archive.
// Response: 200 {"projectName","workingDir"} on success; 400 invalid
// name / bad archive, 409 project already exists, 500 on I/O failure.
func (ctx *Context) OfficeUploadCodebase(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCodebaseUpload)

	mr, err := r.MultipartReader()
	if err != nil {
		request.WriteError(w, http.StatusBadRequest, "expected multipart/form-data")
		return
	}

	var projectName string
	tmpPath, copyErr := streamUploadToTemp(mr, &projectName)
	if tmpPath != "" {
		defer os.Remove(tmpPath)
	}
	if copyErr != nil {
		// MaxBytesReader trips here when the upload exceeds the cap.
		request.WriteError(w, http.StatusBadRequest, copyErr.Error())
		return
	}
	if projectName == "" || tmpPath == "" {
		request.WriteError(w, http.StatusBadRequest, "missing projectName or file")
		return
	}

	target, prepErr := office.PrepareZipProject(projectName, tmpPath, office.DefaultGitRunner, ctx.emit)
	if prepErr != nil {
		switch {
		case errors.Is(prepErr, office.ErrInvalidProjectName):
			request.WriteError(w, http.StatusBadRequest, prepErr.Error())
		case errors.Is(prepErr, office.ErrProjectExists):
			request.WriteError(w, http.StatusConflict, prepErr.Error())
		case errors.Is(prepErr, office.ErrZipTooLarge):
			request.WriteError(w, http.StatusBadRequest, prepErr.Error())
		default:
			request.WriteError(w, http.StatusInternalServerError, "failed to extract archive: "+prepErr.Error())
		}
		return
	}

	request.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"projectName": projectName,
		"workingDir":  target,
	})
}

// streamUploadToTemp walks the multipart parts, capturing the
// "projectName" field into name and streaming the "file" part to a temp
// file on disk. It returns the temp path (which the caller must remove)
// and any copy error. The temp path is returned even on error so a
// partial file is still cleaned up.
func streamUploadToTemp(mr *multipart.Reader, name *string) (string, error) {
	var tmpPath string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return tmpPath, err
		}
		switch part.FormName() {
		case "projectName":
			buf, readErr := io.ReadAll(io.LimitReader(part, 256))
			part.Close()
			if readErr != nil {
				return tmpPath, readErr
			}
			*name = string(buf)
		case "file":
			tmp, createErr := os.CreateTemp("", "jarvis-codebase-*.zip")
			if createErr != nil {
				part.Close()
				return tmpPath, createErr
			}
			tmpPath = tmp.Name()
			_, copyErr := io.Copy(tmp, part)
			tmp.Close()
			part.Close()
			if copyErr != nil {
				return tmpPath, copyErr
			}
		default:
			part.Close()
		}
	}
	return tmpPath, nil
}
