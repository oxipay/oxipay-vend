package terminal

import (
	"database/sql"
	"errors"
)

// Terminal terminal mapping
type Register struct {
	FxlRegisterID       string // Oxipay registerid
	FxlSellerID         string
	FxlDeviceSigningKey string
	Origin              string
	VendRegisterID      string
}

// Terminal terminal mapping
type Terminal struct {
	Db *sql.DB
}

// Db connection to the database
var db *sql.DB

// NewTerminal Used to marshall the DB connection
func NewTerminal(db *sql.DB) *Terminal {
	return &Terminal{
		Db: db,
	}
}

// NewRegister returns a Pointer to a terminal
func NewRegister(key string, deviceID string, merchantID string, origin string, registerID string) *Register {
	return &Register{
		FxlDeviceSigningKey: key,
		FxlRegisterID:       deviceID,
		FxlSellerID:         merchantID, // Oxipay Merchant No
		Origin:              origin,     // Vend Website
		VendRegisterID:      registerID, // Vend Register ID
	}
}

//Save will save the terminal to the database
func (t Terminal) Save(user string, register *Register) (bool, error) {
	query := `INSERT INTO 
		oxipay_vend_map  
		(
			fxl_register_id,
			fxl_seller_id,
			fxl_device_signing_key,
			origin_domain, 
			vend_register_id,
			created_by
		) VALUES (?, ?, ?, ?, ?, ?) `

	stmt, err := t.Db.Prepare(query)

	if err != nil {
		return false, err
	}

	defer stmt.Close()

	_, err = stmt.Exec(
		newNullString(register.FxlRegisterID),
		newNullString(register.FxlSellerID),
		newNullString(register.FxlDeviceSigningKey),
		newNullString(register.Origin),
		newNullString(register.VendRegisterID),
		newNullString(user),
	)

	if err != nil {
		return false, err
	}

	return true, nil
}

// GetRegister will return a registered terminal for the the domain & vendregister_id combo
func (t Terminal) GetRegister(originDomain string, vendRegisterID string) (*Register, error) {

	sql := `SELECT 
			 fxl_register_id, 
			 fxl_seller_id,
			 fxl_device_signing_key, 
			 origin_domain,
			 vend_register_id
			FROM 
				oxipay_vend_map 
			WHERE 
				origin_domain = ? 
			AND
				vend_register_id = ? 
			AND 1=1`

	rows, err := t.Db.Query(sql, originDomain, vendRegisterID)

	var register = new(Register)
	noRows := 0

	for rows.Next() {
		noRows++
		err = rows.Scan(
			&register.FxlRegisterID,
			&register.FxlSellerID,
			&register.FxlDeviceSigningKey,
			&register.Origin,
			&register.VendRegisterID,
		)

	}
	if err != nil {
		return register, err
	}

	if noRows < 1 {
		return nil, errors.New("Unable to find a matching terminal ")
	}

	return register, err
}

func newNullString(s string) sql.NullString {
	if len(s) == 0 {
		return sql.NullString{}
	}
	return sql.NullString{
		String: s,
		Valid:  true,
	}
}
