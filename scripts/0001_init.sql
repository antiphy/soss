-- SOSS 元数据初始化脚本（design §15 ~ §17, §76）
-- 由 dal/sqlite 迁移器按文件名顺序执行，禁止手工修改线上库结构。

CREATE TABLE IF NOT EXISTS buckets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    class INTEGER NOT NULL CHECK (class IN (1, 2)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS objects (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    bucket_id INTEGER NOT NULL,
    object_key TEXT NOT NULL,
    latest_version INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(bucket_id, object_key),
    FOREIGN KEY(bucket_id) REFERENCES buckets(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS object_versions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    object_id INTEGER NOT NULL,
    version INTEGER NOT NULL,
    data_file TEXT NOT NULL,
    data_offset INTEGER NOT NULL,
    data_size INTEGER NOT NULL,
    record_size INTEGER NOT NULL,
    filename TEXT,
    content_type TEXT,
    etag TEXT,
    checksum TEXT,
    created_at INTEGER NOT NULL,
    UNIQUE(object_id, version),
    FOREIGN KEY(object_id) REFERENCES objects(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS storage_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    filename TEXT NOT NULL UNIQUE,
    capacity_bytes INTEGER NOT NULL,
    write_offset INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    status INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_objects_bucket
    ON objects(bucket_id);

CREATE INDEX IF NOT EXISTS idx_object_versions_object
    ON object_versions(object_id);

CREATE INDEX IF NOT EXISTS idx_object_versions_created
    ON object_versions(object_id, version DESC);
