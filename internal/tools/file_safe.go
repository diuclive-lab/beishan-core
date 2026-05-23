package tools

import (
	"fmt"
	"path/filepath"
	"sync"
)

// ─── File Operation Validation ─────────────────────────────────────────

type fileOpType string

const (
	opRead  fileOpType = "read"
	opWrite fileOpType = "write"
	opDelete fileOpType = "delete"
	opList   fileOpType = "list"
)

var allowedOps = map[fileOpType]bool{
	opRead:  true,
	opWrite: true,
	opDelete: true,
	opList:   true,
}

// validateFileOp checks that the operation type and path are allowed.
func validateFileOp(op fileOpType, path string) error {
	if !allowedOps[op] {
		return fmt.Errorf("不支持的ファイル操作: %s", op)
	}
	return isSafePath(path)
}

// ─── File Locking ──────────────────────────────────────────────────────

var (
	fileLocks   = make(map[string]*sync.Mutex)
	fileLocksMu sync.Mutex
)

func getFileLock(path string) *sync.Mutex {
	absPath, _ := filepath.Abs(path)
	fileLocksMu.Lock()
	defer fileLocksMu.Unlock()
	if _, ok := fileLocks[absPath]; !ok {
		fileLocks[absPath] = &sync.Mutex{}
	}
	return fileLocks[absPath]
}

// lockFile locks a file path for exclusive access.
// Returns a function to unlock, usable with defer.
func lockFile(path string) (func(), error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("锁文件失败: %w", err)
	}
	if err := isSafePath(absPath); err != nil {
		return nil, err
	}
	getFileLock(absPath).Lock()
	return func() { getFileLock(absPath).Unlock() }, nil
}

// ─── Tool Registration ─────────────────────────────────────────────────

func registerFileSafeTools() {
	Register("validate_file_op", "Validate a file operation (read/write/delete).",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"op", "path"},
			"properties": map[string]interface{}{
				"op":   stringParam("Operation type: read/write/delete"),
				"path": stringParam("File path to validate"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			op, _ := args["op"].(string)
			path, _ := args["path"].(string)
			if op == "" || path == "" {
				return errorResult("op and path required")
			}
			if err := validateFileOp(fileOpType(op), path); err != nil {
				return errorResult(err.Error())
			}
			return successResult(fmt.Sprintf("操作 %s → %s 允许", op, path))
		},
	)

	Register("lock_file", "Lock a file for exclusive access.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"path"},
			"properties": map[string]interface{}{
				"path": stringParam("File path to lock"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			path, _ := args["path"].(string)
			if path == "" {
				return errorResult("path required")
			}
			unlock, err := lockFile(path)
			if err != nil {
				return errorResult(err.Error())
			}
			_ = unlock // caller holds the lock; stored in workflow state
			return successResult(fmt.Sprintf("已锁定: %s", path))
		},
	)

	Register("unlock_file", "Unlock a previously locked file.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"path"},
			"properties": map[string]interface{}{
				"path": stringParam("File path to unlock"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			path, _ := args["path"].(string)
			if path == "" {
				return errorResult("path required")
			}
			absPath, _ := filepath.Abs(path)
			fileLocksMu.Lock()
			mu, ok := fileLocks[absPath]
			fileLocksMu.Unlock()
			if !ok {
				return successResult(fmt.Sprintf("文件未锁定: %s", path))
			}
			mu.Unlock()
			return successResult(fmt.Sprintf("已解锁: %s", path))
		},
	)
}

