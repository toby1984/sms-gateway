package serialportdiscovery

import (
	"code-sourcery.de/sms-gateway/common"
	"code-sourcery.de/sms-gateway/logger"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var log = logger.GetLogger("portdiscovery")

func DiscoverUsbInterfaces(deviceId common.UsbDeviceId) ([]string, error) {
	var ifaces []string

	// Iterate through all USB ifaces
	matches, err := filepath.Glob("/sys/bus/usb/devices/*")
	if err != nil {
		log.Error("Error globbing USB interfaces: " + err.Error())
		return []string{}, err
	}
	log.Debug("Checking " + strconv.Itoa(len(matches)) + " USB devices")

	vendorId := fmt.Sprintf("%04x", deviceId.VendorId)
	productId := fmt.Sprintf("%04x", deviceId.ProductId)

	deviceFound := false

	for _, deviceDir := range matches {
		vendorID, err := os.ReadFile(filepath.Join(deviceDir, "idVendor"))
		if err != nil {
			continue
		}
		productID, err := os.ReadFile(filepath.Join(deviceDir, "idProduct"))
		if err != nil {
			continue
		}

		vID := strings.TrimSpace(string(vendorID))
		pID := strings.TrimSpace(string(productID))

		log.Debug("Checking USB device '" + vID + "', '" + pID + "'")

		if vID == vendorId && pID == productId {
			deviceFound = true
			// Found the device, now look for the ttyUSB node
			ttyGlob := filepath.Join(deviceDir, "*/ttyUSB*")
			ttyMatches, err := filepath.Glob(ttyGlob)
			if err != nil {
				fmt.Printf("Error globbing ttyUSB interfaces: %v\n", err)
				continue
			}

			for _, ttyPath := range ttyMatches {
				deviceName := filepath.Base(ttyPath)
				ifaces = append(ifaces, "/dev/"+deviceName)
			}
		}
	}
	if !deviceFound {
		return []string{}, errors.New("Found no USB device with ID " + deviceId.String())
	}
	// sort alphabetically to have a stable interface order
	// as config.getSerialPort() will index into the returned []string slice
	sort.Strings(ifaces)
	for i, iface := range ifaces {
		log.Debug("Discovered interface #" + strconv.Itoa(i) + " : " + iface)
	}
	return ifaces, nil
}
