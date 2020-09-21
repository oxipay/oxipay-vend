-- Deploy vendproxy:sessions to mysql
use vend;
BEGIN;

DROP TABLE IF EXISTS `sessions`; 
CREATE TABLE sessions (
	id INT NOT NULL AUTO_INCREMENT,
	session_data LONGBLOB,
    created_on TIMESTAMP DEFAULT NOW(),
	modified_on TIMESTAMP NOT NULL DEFAULT NOW() ON UPDATE CURRENT_TIMESTAMP,
    expires_on TIMESTAMP DEFAULT NOW(),
     PRIMARY KEY(`id`)
 ) engine=InnoDB, COMMENT = 'This stores http sessions and is required by the session store handler';

COMMIT;
