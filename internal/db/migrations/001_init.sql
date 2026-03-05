CREATE TABLE IF NOT EXISTS users (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  telegram_id BIGINT UNIQUE,
  external_id VARCHAR(255) UNIQUE,
  profile_json JSON,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sessions (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  user_id BIGINT NOT NULL,
  channel VARCHAR(32) NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'active',
  summary TEXT,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_user_channel (user_id, channel),
  FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS messages (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  session_id BIGINT NOT NULL,
  role VARCHAR(16) NOT NULL,
  content MEDIUMTEXT NOT NULL,
  metadata_json JSON,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_session_created (session_id, created_at),
  FOREIGN KEY (session_id) REFERENCES sessions(id)
);

CREATE TABLE IF NOT EXISTS memories (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  user_id BIGINT NOT NULL,
  type VARCHAR(32) NOT NULL,
  content TEXT NOT NULL,
  importance INT DEFAULT 0,
  embedding BLOB,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_user_created (user_id, created_at),
  FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS skill_sources (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  source_type VARCHAR(32) NOT NULL,
  install_method VARCHAR(32) NOT NULL,
  source_url TEXT NOT NULL,
  source_ref VARCHAR(128) DEFAULT '',
  source_subdir VARCHAR(255) DEFAULT '',
  status VARCHAR(16) DEFAULT 'active',
  version VARCHAR(64),
  last_sync_at TIMESTAMP NULL,
  last_error TEXT,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_source (source_type(32), source_url(255), source_ref(128), source_subdir(255))
);

CREATE TABLE IF NOT EXISTS skills_registry (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  source_id BIGINT NOT NULL,
  name VARCHAR(128) NOT NULL,
  description TEXT NOT NULL,
  version VARCHAR(64),
  skill_path TEXT NOT NULL,
  content_hash CHAR(64),
  status VARCHAR(16) DEFAULT 'active',
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_source_name (source_id, name),
  FOREIGN KEY (source_id) REFERENCES skill_sources(id)
);

CREATE TABLE IF NOT EXISTS tool_audit (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  session_id BIGINT NULL,
  user_id BIGINT NULL,
  tool VARCHAR(32) NOT NULL,
  path TEXT,
  allowed TINYINT(1) NOT NULL,
  status VARCHAR(16) NOT NULL,
  reason TEXT,
  metadata_json JSON,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
