package handlers

import (
	"service/internal/fs"
	"service/internal/logger"
	"service/internal/response"
	"encoding/json"
	"net/http"
)

// ОБРАБОТЧИКИ HTTP ЗАПРОСОВ
// #region [GET]
func ReadFileHandler(w http.ResponseWriter, r *http.Request) {
	relPath := r.URL.Query().Get("path")
	absPath, err := fs.PathGuard(fs.RootDir, relPath)

	if err != nil {
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}
	if !fs.ExistenceCheck(absPath) {
		response.Send(w, response.Response{Success: false, Error: "File not found"}, http.StatusBadRequest)
		return
	}

	content, err := fs.Read(absPath)
	if err != nil {
		logger.Error(r, err)
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}
	response.Send(w, response.Response{Success: true, Content: content}, http.StatusOK)

}

func GetInfoHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	absPath, err := fs.PathGuard(fs.RootDir, path)

	if err != nil {
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	if !fs.ExistenceCheck(absPath) {
		response.Send(w, response.Response{Success: false, Error: "File not found"}, http.StatusBadRequest)
		return
	}

	fileInfo, err := fs.GetFileInfoByPath(absPath)
	if err != nil {
		logger.Error(r, err)
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}

	response.Send(w, response.Response{Success: true, Content: fileInfo}, http.StatusOK)
}

func GetListHandler(w http.ResponseWriter, r *http.Request) {

	path := r.URL.Query().Get("path")
	absPath, err := fs.PathGuard(fs.RootDir, path)
	if err != nil {
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	if !fs.ExistenceCheck(absPath) {
		response.Send(w, response.Response{Success: false, Error: "Directory not found"}, http.StatusBadRequest)
		return
	}

	if isDir, err := fs.IsDir(absPath); err != nil {
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	} else if !isDir {
		response.Send(w, response.Response{Success: false, Error: "Not a directory"}, http.StatusBadRequest)
		return
	}

	// name, size, date, type
	sortBy := r.URL.Query().Get("sort")
	// asc, desc
	order := r.URL.Query().Get("order")

	files, err := fs.GetFilesList(absPath, sortBy, order)
	if err != nil {
		logger.Error(r, err)
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}

	response.Send(w, response.Response{Success: true, Content: files}, http.StatusOK)

}

func SearchByNameHandler(w http.ResponseWriter, r *http.Request) {
	dirPath := r.URL.Query().Get("path")
	name := r.URL.Query().Get("name")

	if name == "" {
		response.Send(w, response.Response{Success: false, Error: "Name is required"}, http.StatusBadRequest)
		return
	}

	absDirPath, err := fs.PathGuard(fs.RootDir, dirPath)

	if err != nil {
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}
	if isDir, err := fs.IsDir(absDirPath); err != nil {
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	} else if !fs.ExistenceCheck(absDirPath) || !isDir {
		response.Send(w, response.Response{Success: false, Error: "Directory not found"}, http.StatusBadRequest)
		return
	}

	results, err := fs.SearchFilesByName(absDirPath, name)
	if err != nil {
		logger.Error(r, err)
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}

	response.Send(w, response.Response{Success: true, Content: results}, http.StatusOK)
}

func GetFileStatsHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	absPath, err := fs.PathGuard(fs.RootDir, path)
	if err != nil {
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	if !fs.ExistenceCheck(absPath) {
		response.Send(w, response.Response{Success: false, Error: "File not found"}, http.StatusBadRequest)
		return
	}

	if isDir, err := fs.IsDir(absPath); err != nil {
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	} else if isDir {
		response.Send(w, response.Response{Success: false, Error: "Path is a directory"}, http.StatusBadRequest)
		return
	}

	stats, err := fs.GetFileStats(absPath)
	if err != nil {
		logger.Error(r, err)
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}

	response.Send(w, response.Response{Success: true, Content: stats}, http.StatusOK)
}

//#endregion

// #region [PUT]


//#endregion

// #region [PATCH]
func WriteHandler(w http.ResponseWriter, r *http.Request) {
	type request struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Append  bool   `json:"append"`
	}

	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Send(w, response.Response{Success: false, Error: "Bad Request"}, http.StatusBadRequest)
		return
	}

	absPath, err := fs.PathGuard(fs.RootDir, req.Path)
	if err != nil {
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	if !fs.ExistenceCheck(absPath) {
		response.Send(w, response.Response{Success: false, Error: "File not found"}, http.StatusBadRequest)
		return
	}

	if err := fs.Write(absPath, req.Content, req.Append); err != nil {
		logger.Error(r, err)
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}

	response.Send(w, response.Response{Success: true}, http.StatusOK)
}

//#endregion

// #region [POST]
func CreateFileHandler(w http.ResponseWriter, r *http.Request) {
	type request struct {
		Path    string `json:"path"`
		Rewrite bool   `json:"rewrite"`
	}

	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Send(w, response.Response{Success: false, Error: "Bad Request"}, http.StatusBadRequest)
	}

	// проверка безопасности пути источника
	absPath, err := fs.PathGuard(fs.RootDir, req.Path)
	if err != nil {
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	// проверка существования файла с таким же именем и значения флага перезаписи
	if fs.ExistenceCheck(absPath) && !req.Rewrite {
		response.Send(w, response.Response{Success: false, Error: "File with the same name already exists"}, http.StatusBadRequest)
		return
	}

	if err := fs.CreateFile(absPath); err != nil {
		logger.Error(r, err)
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}

	response.Send(w, response.Response{Success: true}, http.StatusOK)

}

func CreateDirHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")

	// проверка безопасности пути источника
	absPath, err := fs.PathGuard(fs.RootDir, path)
	if err != nil {
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	// проверка существования директории с таким же именем
	if fs.ExistenceCheck(absPath) {
		response.Send(w, response.Response{Success: false, Error: "Directory with the same name already exists"}, http.StatusBadRequest)
		return
	}

	if err := fs.CreateDir(absPath); err != nil {
		logger.Error(r, err)
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}

	response.Send(w, response.Response{Success: true}, http.StatusOK)

}

func CopyHandler(w http.ResponseWriter, r *http.Request) {
	type request struct {
		SrcPath string `json:"srcpath"`
		DstPath string `json:"dstpath"`
		Rewrite bool   `json:"rewrite"`
	}

	var req request

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	// проверка безопасности пути источника
	absSrcPath, err := fs.PathGuard(fs.RootDir, req.SrcPath)
	if err != nil { // || absSrcPath == "" {
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	//проверка отсутствия файла
	if !fs.ExistenceCheck(absSrcPath) {
		response.Send(w, response.Response{Success: false, Error: "Source file not found"}, http.StatusBadRequest)
		return
	}

	// проверка безопасности пути назначения
	absDstPath, err := fs.PathGuard(fs.RootDir, req.DstPath)
	if err != nil {
		response.Send(w, response.Response{Success: false, Error: "Invalid path"}, http.StatusBadRequest)
		return
	}

	// проверка отсутствия вложенности путей src <- dst
	if fs.SublevelCheck(absSrcPath, absDstPath) {
		response.Send(w, response.Response{Success: false, Error: "Cannot copy a folder to itself"}, http.StatusBadRequest)
		return
	}

	// проверка существования файла и значения флага перезаписи
	if fs.ExistenceCheck(absDstPath) && !req.Rewrite && absSrcPath != absDstPath {
		response.Send(w, response.Response{Success: false, Error: "File with the same name already exists"}, http.StatusBadRequest)
		return
	}

	if err := fs.CopyRecursive(absSrcPath, absDstPath); err != nil {
		logger.Error(r, err)
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}

	response.Send(w, response.Response{Success: true}, http.StatusOK)

}

func MoveHandler(w http.ResponseWriter, r *http.Request) {
	type request struct {
		SrcPath string `json:"srcpath"`
		DstPath string `json:"dstpath"`
		Rewrite bool   `json:"rewrite"`
	}

	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	absSrcPath, err := fs.PathGuard(fs.RootDir, req.SrcPath)
	if err != nil {
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	if !fs.ExistenceCheck(absSrcPath) {
		response.Send(w, response.Response{Success: false, Error: "Source file not found"}, http.StatusBadRequest)
		return
	}

	absDstPath, err := fs.PathGuard(fs.RootDir, req.DstPath)
	if err != nil {
		response.Send(w, response.Response{Success: false, Error: "Invalid path"}, http.StatusBadRequest)
		return
	}

	// проверка отсутствия вложенности путей src <- dst
	if fs.SublevelCheck(absSrcPath, absDstPath) {
		response.Send(w, response.Response{Success: false, Error: "Cannot move a folder to itself"}, http.StatusBadRequest)
		return
	}

	// проверка существования файла и значения флага перезаписи
	if fs.ExistenceCheck(absDstPath) && !req.Rewrite && absSrcPath != absDstPath {
		response.Send(w, response.Response{Success: false, Error: "File with the same name already exists"}, http.StatusBadRequest)
		return
	}

	if err := fs.Move(absSrcPath, absDstPath); err != nil {
		logger.Error(r, err)
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}

	response.Send(w, response.Response{Success: true}, http.StatusOK)

}

//#endregion

// #region [DELETE]
func DeleteHandler(w http.ResponseWriter, r *http.Request) {
	type request struct {
		Files []string `json:"files"`
	}

	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusBadRequest)
		return
	}

	if err := fs.Delete(req.Files); err != nil {
		logger.Error(r, err)
		response.Send(w, response.Response{Success: false, Error: err.Error()}, http.StatusInternalServerError)
		return
	}
	
	response.Send(w, response.Response{Success: true}, http.StatusOK)
}

//#endregion
