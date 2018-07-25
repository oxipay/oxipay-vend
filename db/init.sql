create database vend;
use vend;

DROP TABLE IF EXISTS `oxipay_vend_map`;
--
create table oxipay_vend_map (
    id int NOT NULL  auto_increment,
    fxl_register_id text NOT NULL,
    fxl_seller_id text NOT NULL,
    fxl_device_signing_key text,
    origin_domain text NOT NULL ,
    vend_register_id text NOT NULL,
    created_date datetime DEFAULT CURRENT_TIMESTAMP,
    created_by text NOT NULL ,
    modified_date datetime,
    modified_by text,
    primary key(id)
) engine=InnoDB;


-- insert test records

INSERT INTO oxipay_vend_map (
    fxl_register_id,
    fxl_seller_id,
    fxl_device_signing_key, 
    origin_domain,
    vend_register_id,
    created_by
) VALUES (
    'Oxipos',
    '30188105',
    'JCjbPGtuniWr',
    'https://sand.oxipay.com.au',
    '57d863b4-4ae0-492c-b44a-326db76f7dac',
    'andrewm'
);

