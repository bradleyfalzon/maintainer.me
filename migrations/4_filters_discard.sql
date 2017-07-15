-- +migrate Up
ALTER TABLE `filters` ADD COLUMN on_match_discard TINYINT NOT NULL DEFAULT 0 AFTER user_id;

-- +migrate Down
ALTER TABLE `filters` DROP COLUMN on_match_discard;
