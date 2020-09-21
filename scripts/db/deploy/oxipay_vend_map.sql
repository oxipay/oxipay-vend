-- Deploy vendproxy:oxipay_vend_map to mysql
use vend;
BEGIN;

create table oxipay_vend_map (
    id int NOT NULL  auto_increment,
    fxl_register_id varchar(255) NOT NULL COMMENT 'i.e oxipay/ezi-pay Device ID',
    fxl_seller_id varchar(255) NOT NULL COMMENT 'i.e Merchant ID in oxipay/ezi-pay',
    fxl_device_signing_key varchar(255) COMMENT 'i.e Device specific signing key allocated by CreateKey',
    origin_domain varchar(255) NOT NULL COMMENT 'Vend origin provided in the initial request',
    vend_register_id varchar(255) NOT NULL COMMENT 'Unique Register ID from Vend',
    created_date datetime DEFAULT CURRENT_TIMESTAMP,
    created_by text NOT NULL ,
    modified_date datetime,
    modified_by text,
    primary key(id)
     
) engine=InnoDB;

COMMIT;