create database vend;
use vend;

DROP TABLE IF EXISTS `oxipay_vend_map`;
--
create table oxipay_vend_map (
    id int NOT NULL  auto_increment,
    fxl_register_id  text,
    internal_signing_key text,
    origin_domain text NOT NULL ,
    vend_register_id text,
    created_date datetime,
    created_by text NOT NULL ,
    modified_date datetime,
    modified_by text,
    primary key(id)
) engine=InnoDB;


-- insert test records
INSERT INTO oxipay_vend_map (
    fxl_register_id,
    internal_signing_key, 
    origin_domain,
    vend_register_id,
    created_by
) VALUES (
    '2341341',
    'asdkjfhasdfasdf',
    'foo.com',
    'registerfoo',
    'andrewm'
);
