package usb

import (
	"testing"

	"github.com/qubesome/libudev/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// yubikeyTree returns a flattened device list mimicking what libudev's
// ScanDevices produces for a YubiKey: a top-level USB device node carrying
// busnum/devnum/product attrs, two interfaces, and two hidraw descendants.
func yubikeyTree() []*types.Device {
	root := &types.Device{
		Devpath:   "/sys/devices/pci0000:00/usb5/5-2/5-2.2/5-2.2.3",
		VendorID:  "1050",
		ProductID: "0407",
		Attrs: map[string]string{
			"busnum":   "5",
			"devnum":   "12",
			"product":  "YubiKey OTP+FIDO+CCID",
			"idVendor": "1050",
		},
	}
	iface0 := &types.Device{
		Devpath:   root.Devpath + "/5-2.2.3:1.0",
		VendorID:  "1050",
		ProductID: "0407",
		Attrs:     map[string]string{},
	}
	iface1 := &types.Device{
		Devpath:   root.Devpath + "/5-2.2.3:1.1",
		VendorID:  "1050",
		ProductID: "0407",
		Attrs:     map[string]string{},
	}
	hid9 := &types.Device{
		Devpath:   iface0.Devpath + "/0003:1050:0407.000A/hidraw/hidraw9",
		VendorID:  "1050",
		ProductID: "0407",
		Attrs:     map[string]string{},
	}
	hid10 := &types.Device{
		Devpath:   iface1.Devpath + "/0003:1050:0407.000B/hidraw/hidraw10",
		VendorID:  "1050",
		ProductID: "0407",
		Attrs:     map[string]string{},
	}

	iface0.Children = []*types.Device{hid9}
	iface1.Children = []*types.Device{hid10}
	root.Children = []*types.Device{iface0, iface1}

	return []*types.Device{root, iface0, iface1, hid9, hid10}
}

func TestSelectDevices_MatchByVendorProductID(t *testing.T) {
	t.Parallel()

	got, err := selectDevices(yubikeyTree(), []string{"1050:0407"})

	require.NoError(t, err)
	assert.Equal(t, []string{
		"/dev/bus/usb/005/012",
		"/dev/hidraw9",
		"/dev/hidraw10",
	}, got)
}

func TestSelectDevices_MatchByIDIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	dev := &types.Device{
		Devpath:   "/sys/devices/usb1/1-1",
		VendorID:  "abcd",
		ProductID: "00ef",
		Attrs:     map[string]string{"busnum": "1", "devnum": "2"},
	}

	got, err := selectDevices([]*types.Device{dev}, []string{"ABCD:00EF"})

	require.NoError(t, err)
	assert.Equal(t, []string{"/dev/bus/usb/001/002"}, got)
}

func TestSelectDevices_MatchByProductNamePrefix(t *testing.T) {
	t.Parallel()

	got, err := selectDevices(yubikeyTree(), []string{"YubiKey"})

	require.NoError(t, err)
	assert.Equal(t, []string{
		"/dev/bus/usb/005/012",
		"/dev/hidraw9",
		"/dev/hidraw10",
	}, got)
}

func TestSelectDevices_IDFormTakesPrecedenceOverProductName(t *testing.T) {
	t.Parallel()

	// The product name is literally "1050:0407" but the IDs differ. An
	// ID-shaped entry must be matched as an ID, not as a product name.
	dev := &types.Device{
		Devpath:   "/sys/devices/usb1/1-1",
		VendorID:  "9999",
		ProductID: "8888",
		Attrs: map[string]string{
			"busnum":  "1",
			"devnum":  "2",
			"product": "1050:0407",
		},
	}

	got, err := selectDevices([]*types.Device{dev}, []string{"1050:0407"})

	require.NoError(t, err)
	assert.Empty(t, got)
}

// hubWithDownstream models a hub whose downstream USB device owns a hidraw
// node. A hub must not claim hidraw nodes belonging to downstream devices.
func hubWithDownstream() []*types.Device {
	hub := &types.Device{
		Devpath:   "/sys/devices/pci0000:00/usb9",
		VendorID:  "1d6b",
		ProductID: "0002",
		Attrs:     map[string]string{"busnum": "9", "devnum": "1", "product": "xHCI Host Controller"},
	}
	downstream := &types.Device{
		Devpath:   hub.Devpath + "/9-1",
		VendorID:  "046d",
		ProductID: "c090",
		Attrs:     map[string]string{"busnum": "9", "devnum": "5", "product": "G703"},
	}
	iface := &types.Device{
		Devpath: downstream.Devpath + "/9-1:1.0",
		Attrs:   map[string]string{},
	}
	hid := &types.Device{
		Devpath: iface.Devpath + "/0003:046d:c090.0001/hidraw/hidraw9",
		Attrs:   map[string]string{},
	}

	iface.Children = []*types.Device{hid}
	downstream.Children = []*types.Device{iface}
	hub.Children = []*types.Device{downstream}

	return []*types.Device{hub, downstream, iface, hid}
}

func TestSelectDevices_DoesNotCrossIntoDownstreamUSBDevices(t *testing.T) {
	t.Parallel()

	got, err := selectDevices(hubWithDownstream(), []string{"1d6b:0002"})

	require.NoError(t, err)
	assert.Equal(t, []string{"/dev/bus/usb/009/001"}, got)
}

func TestSelectDevices_DownstreamDeviceKeepsOwnHidraw(t *testing.T) {
	t.Parallel()

	got, err := selectDevices(hubWithDownstream(), []string{"046d:c090"})

	require.NoError(t, err)
	assert.Equal(t, []string{"/dev/bus/usb/009/005", "/dev/hidraw9"}, got)
}

func TestSelectDevices_NoMatch(t *testing.T) {
	t.Parallel()

	got, err := selectDevices(yubikeyTree(), []string{"dead:beef"})

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestSelectDevices_SkipsNonUSBDevices(t *testing.T) {
	t.Parallel()

	// An interface with an inherited VendorID but no busnum/devnum is not a
	// USB device node and must not be selected.
	iface := &types.Device{
		Devpath:   "/sys/devices/usb1/1-1/1-1:1.0",
		VendorID:  "1050",
		ProductID: "0407",
		Attrs:     map[string]string{},
	}

	got, err := selectDevices([]*types.Device{iface}, []string{"1050:0407"})

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestSelectDevices_InvalidBusnumErrors(t *testing.T) {
	t.Parallel()

	dev := &types.Device{
		Devpath:   "/sys/devices/usb1/1-1",
		VendorID:  "1050",
		ProductID: "0407",
		Attrs:     map[string]string{"busnum": "notanumber", "devnum": "2"},
	}

	_, err := selectDevices([]*types.Device{dev}, []string{"1050:0407"})

	require.Error(t, err)
}

func TestListDevices(t *testing.T) {
	t.Parallel()

	got := listDevices(yubikeyTree())

	assert.Equal(t, []Info{
		{
			VendorID:  "1050",
			ProductID: "0407",
			Product:   "YubiKey OTP+FIDO+CCID",
			Paths: []string{
				"/dev/bus/usb/005/012",
				"/dev/hidraw9",
				"/dev/hidraw10",
			},
		},
	}, got)
}

func TestListDevices_SortedByID(t *testing.T) {
	t.Parallel()

	a := &types.Device{
		Devpath:   "/sys/devices/usb1/1-1",
		VendorID:  "1050",
		ProductID: "0407",
		Attrs:     map[string]string{"busnum": "1", "devnum": "3", "product": "YubiKey"},
	}
	b := &types.Device{
		Devpath:   "/sys/devices/usb1/1-2",
		VendorID:  "046d",
		ProductID: "c52b",
		Attrs:     map[string]string{"busnum": "1", "devnum": "2", "product": "Unifying Receiver"},
	}

	got := listDevices([]*types.Device{a, b})

	require.Len(t, got, 2)
	assert.Equal(t, "046d", got[0].VendorID)
	assert.Equal(t, "1050", got[1].VendorID)
}

func TestParseVendorProduct(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in           string
		vendor, prod string
		ok           bool
	}{
		{"1050:0407", "1050", "0407", true},
		{"ABCD:00EF", "ABCD", "00EF", true},
		{"105:0407", "", "", false},
		{"1050-0407", "", "", false},
		{"gggg:0407", "", "", false},
		{"YubiKey", "", "", false},
		{"1050:0407:extra", "", "", false},
	}

	for _, tc := range tests {
		vendor, prod, ok := parseVendorProduct(tc.in)
		assert.Equal(t, tc.ok, ok, tc.in)
		assert.Equal(t, tc.vendor, vendor, tc.in)
		assert.Equal(t, tc.prod, prod, tc.in)
	}
}
