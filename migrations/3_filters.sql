-- +migrate Up
CREATE TABLE filters (
	id INT UNSIGNED AUTO_INCREMENT,
	user_id INT UNSIGNED NOT NULL,
	created_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	PRIMARY KEY (`id`),
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=innodb;

CREATE TABLE conditions (
	id INT UNSIGNED AUTO_INCREMENT,
	filter_id INT UNSIGNED NOT NULL,
    negate TINYINT NOT NULL DEFAULT 0,
    `type` VARCHAR(64) NOT NULL DEFAULT '',
    payload_action VARCHAR(64) NOT NULL DEFAULT '',
    payload_issue_label VARCHAR(64) NOT NULL DEFAULT '',
    payload_issue_milestone_title VARCHAR(64) NOT NULL DEFAULT '',
    payload_issue_title_regexp VARCHAR(64) NOT NULL DEFAULT '',
    payload_issue_body_regexp VARCHAR(64) NOT NULL DEFAULT '',
    public TINYINT NOT NULL DEFAULT 0, -- 0 = any, 1 = public, 2 = private
    organization_id INT NOT NULL DEFAULT 0,
    repository_id INT NOT NULL DEFAULT 0,
	created_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	PRIMARY KEY (`id`),
    FOREIGN KEY (filter_id) REFERENCES filters (id) ON DELETE CASCADE
) ENGINE=innodb;

-- +migrate Down
DROP TABLE conditions;
DROP TABLE filters;
