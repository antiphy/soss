package sqlite

import (
	"context"
	"database/sql"
	"io/fs"
	"sort"
	"strings"

	"go.uber.org/zap"

	serr "github.com/favos-me/soss/errors"
)

// Migrator 执行 scripts/ 下的 SQL 迁移脚本。
type Migrator struct {
	db     *sql.DB
	fsys   fs.FS
	logger *zap.Logger
}

// NewMigrator 创建迁移器，fsys 应指向 scripts 目录本身。
func NewMigrator(db *sql.DB, fsys fs.FS, logger *zap.Logger) *Migrator {
	return &Migrator{db: db, fsys: fsys, logger: logger}
}

// Init 按文件名顺序执行全部 .sql 脚本。
func (m *Migrator) Init(ctx context.Context) error {
	entries, err := fs.ReadDir(m.fsys, ".")
	if err != nil {
		return serr.Wrap(serr.CodeMetadataError, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		data, err := fs.ReadFile(m.fsys, name)
		if err != nil {
			return serr.Wrap(serr.CodeMetadataError, err)
		}
		for _, stmt := range splitStatements(string(data)) {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if _, err := m.db.ExecContext(ctx, stmt); err != nil {
				m.logger.Error("migration statement failed",
					zap.String("script", name),
					zap.Error(err),
				)
				return serr.Wrap(serr.CodeMetadataError, err)
			}
		}
		m.logger.Info("migration applied", zap.String("script", name))
	}
	return nil
}

// splitStatements 按分号切分 SQL，忽略引号内与注释中的分号。
func splitStatements(script string) []string {
	var out []string
	var buf strings.Builder
	runes := []rune(script)
	for i := 0; i < len(runes); i++ {
		switch {
		case runes[i] == '-' && i+1 < len(runes) && runes[i+1] == '-':
			// 行注释：跳到行尾。
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			buf.WriteRune('\n')
		case runes[i] == '/' && i+1 < len(runes) && runes[i+1] == '*':
			i += 2
			for i+1 < len(runes) && !(runes[i] == '*' && runes[i+1] == '/') {
				i++
			}
			i++
		case runes[i] == '\'' || runes[i] == '"':
			quote := runes[i]
			buf.WriteRune(runes[i])
			i++
			for i < len(runes) {
				buf.WriteRune(runes[i])
				if runes[i] == quote {
					break
				}
				i++
			}
		case runes[i] == ';':
			out = append(out, buf.String())
			buf.Reset()
		default:
			buf.WriteRune(runes[i])
		}
	}
	if strings.TrimSpace(buf.String()) != "" {
		out = append(out, buf.String())
	}
	return out
}
