package common

import "fmt"

type UsbDeviceId struct {
	VendorId  uint16
	ProductId uint16
}

func (id UsbDeviceId) String() string {
	return fmt.Sprintf("%04x:%04x", id.VendorId, id.ProductId)
}
