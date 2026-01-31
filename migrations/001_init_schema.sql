-- Nexo IM Database Schema
-- Version: 1.0.0

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(64) PRIMARY KEY,
    nickname VARCHAR(128) NOT NULL DEFAULT '',
    avatar VARCHAR(512) DEFAULT '',
    password VARCHAR(128) NOT NULL DEFAULT '',
    extra JSON,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Groups table
CREATE TABLE IF NOT EXISTS `groups` (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(128) NOT NULL,
    introduction VARCHAR(512) DEFAULT '',
    avatar VARCHAR(512) DEFAULT '',
    extra JSON,
    status INT DEFAULT 0 COMMENT '0=normal, 1=dismissed',
    creator_user_id VARCHAR(64) NOT NULL,
    group_type INT DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    INDEX idx_creator (creator_user_id),
    INDEX idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Group members table
CREATE TABLE IF NOT EXISTS group_members (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    group_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    group_nickname VARCHAR(128) DEFAULT '',
    group_avatar VARCHAR(512) DEFAULT '',
    extra JSON,
    role_level INT DEFAULT 0 COMMENT '0=member, 1=admin, 2=owner',
    status INT NOT NULL DEFAULT 0 COMMENT '0=normal, 1=left, 2=kicked',
    joined_at BIGINT NOT NULL,
    join_seq BIGINT NOT NULL DEFAULT 0 COMMENT 'first visible seq after joining',
    inviter_user_id VARCHAR(64) DEFAULT '',
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    UNIQUE KEY uk_group_user (group_id, user_id),
    INDEX idx_group_status (group_id, status),
    INDEX idx_user_id (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Conversations table
CREATE TABLE IF NOT EXISTS conversations (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    conversation_id VARCHAR(256) NOT NULL,
    owner_id VARCHAR(64) NOT NULL,
    conversation_type INT NOT NULL COMMENT '1=single, 2=group',
    peer_user_id VARCHAR(64) DEFAULT '',
    group_id VARCHAR(64) DEFAULT '',
    recv_msg_opt INT DEFAULT 0 COMMENT '0=normal, 1=no_notify, 2=not_recv',
    is_pinned TINYINT(1) DEFAULT 0,
    extra JSON,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    UNIQUE KEY uk_owner_conv (owner_id, conversation_id),
    INDEX idx_owner (owner_id),
    INDEX idx_conv_type (conversation_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Conversation sequence table
CREATE TABLE IF NOT EXISTS seq_conversations (
    conversation_id VARCHAR(256) PRIMARY KEY,
    max_seq BIGINT DEFAULT 0,
    min_seq BIGINT DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- User sequence table (per user per conversation)
CREATE TABLE IF NOT EXISTS seq_users (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    conversation_id VARCHAR(256) NOT NULL,
    min_seq BIGINT DEFAULT 0 COMMENT 'min visible seq (set when joining group)',
    max_seq BIGINT DEFAULT 0 COMMENT 'max visible seq (set when leaving group)',
    read_seq BIGINT DEFAULT 0 COMMENT 'last read seq',
    UNIQUE KEY uk_user_conv (user_id, conversation_id),
    INDEX idx_user (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Messages table
CREATE TABLE IF NOT EXISTS messages (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    conversation_id VARCHAR(256) NOT NULL,
    seq BIGINT NOT NULL,
    client_msg_id VARCHAR(64) NOT NULL COMMENT 'client idempotency ID',
    sender_id VARCHAR(64) NOT NULL,
    recv_id VARCHAR(64) DEFAULT '',
    group_id VARCHAR(64) DEFAULT '',
    session_type INT NOT NULL COMMENT '1=single, 2=group',
    msg_type INT NOT NULL COMMENT '1=text, 2=image, 3=video, 4=audio, 5=file, 100=custom',
    content_text TEXT,
    content_image VARCHAR(512) DEFAULT '',
    content_video VARCHAR(512) DEFAULT '',
    content_audio VARCHAR(512) DEFAULT '',
    content_file VARCHAR(512) DEFAULT '',
    content_custom JSON,
    extra JSON,
    send_at BIGINT NOT NULL,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    UNIQUE KEY uk_conv_seq (conversation_id, seq),
    UNIQUE KEY uk_sender_client_msg (sender_id, client_msg_id),
    INDEX idx_sender (sender_id),
    INDEX idx_send_at (send_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
