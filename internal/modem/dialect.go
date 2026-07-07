package modem

import (
	"fmt"
	"strings"

	"github.com/boa-z/vohive/internal/config"
)

type atStringCommand struct {
	Command string
	Parse   func(string) string
}

type atBoolCommand struct {
	Command string
	Parse   func(string) (bool, bool)
}

type atDialect struct {
	Vendor string

	InitCommands []string

	SIMInsertedCommands []atBoolCommand
	ICCIDCommands       []atStringCommand

	ServingCellCommand  string
	ParseServingCell    func(string) (ServingCellLTEInfo, bool)
	NetworkRadioCommand string
	ParseNetworkRadio   func(string) (string, string, string, uint32)

	IMSStatusCommand string
	ParseIMSStatus   func(string) (int, bool)

	USBNetQueryCommand string
	ParseUSBNet        func(string) (int, bool)
	USBNetSetCommand   func(int) (string, bool)

	USBAudioEnableCommand  string
	USBAudioDisableCommand string
	USBAudioQueryCommand   string
	ParseUSBAudioMode      func(string) (bool, int, bool)
}

func resolveATDialect(vendor string) atDialect {
	switch config.NormalizeModuleVendor(vendor) {
	case config.ModuleVendorSIMCOM:
		return simcomATDialect()
	default:
		return quectelATDialect()
	}
}

func quectelATDialect() atDialect {
	return atDialect{
		Vendor: config.ModuleVendorQuectel,
		InitCommands: []string{
			"ATE0",
			"AT+CMGF=0",
			"AT+CNMI=2,1,0,0,0",
			"AT+CLIP=1",
			"AT+QPCMV=1,2",
		},
		SIMInsertedCommands: []atBoolCommand{
			{Command: "AT+QSIMSTAT?", Parse: parseQSIMSTATInserted},
			{Command: "AT+CPIN?", Parse: parseCPINInserted},
		},
		ICCIDCommands: []atStringCommand{
			{Command: "AT+QCCID", Parse: parseQCCID},
		},
		ServingCellCommand:  "AT+QENG=\"servingcell\"",
		ParseServingCell:    parseServingCellLTEInfo,
		NetworkRadioCommand: "AT+QNWINFO",
		ParseNetworkRadio:   parseQNWInfoRadio,
		IMSStatusCommand:    "AT+QIMS?",
		ParseIMSStatus:      parseQIMS,
		USBNetQueryCommand:  "AT+QCFG=\"usbnet\"?",
		ParseUSBNet:         parseUSBNet,
		USBNetSetCommand: func(mode int) (string, bool) {
			return fmt.Sprintf("AT+QCFG=\"usbnet\",%d", mode), true
		},
		USBAudioEnableCommand:  "AT+QPCMV=1,2",
		USBAudioDisableCommand: "AT+QPCMV=0",
		USBAudioQueryCommand:   "AT+QPCMV?",
		ParseUSBAudioMode:      parseQPCMVAudioMode,
	}
}

func simcomATDialect() atDialect {
	return atDialect{
		Vendor: config.ModuleVendorSIMCOM,
		InitCommands: []string{
			"ATE0",
			"AT+CMGF=0",
			"AT+CNMI=2,1,0,0,0",
			"AT+CLIP=1",
			"AT+CPCMREG=1",
		},
		SIMInsertedCommands: []atBoolCommand{
			{Command: "AT+CPIN?", Parse: parseCPINInserted},
		},
		ICCIDCommands: []atStringCommand{
			{Command: "AT+CICCID", Parse: parseICCID},
		},
		ServingCellCommand:     "AT+CPSI?",
		ParseServingCell:       parseCPSIServingCellLTEInfo,
		NetworkRadioCommand:    "AT+CPSI?",
		ParseNetworkRadio:      parseCPSINetworkRadio,
		USBAudioEnableCommand:  "AT+CPCMREG=1",
		USBAudioDisableCommand: "AT+CPCMREG=0,1",
		USBAudioQueryCommand:   "AT+CPCMREG?",
		ParseUSBAudioMode:      parseCPCMREGAudioMode,
	}
}

func (d atDialect) isQuectel() bool {
	return strings.TrimSpace(d.Vendor) == "" || strings.EqualFold(d.Vendor, config.ModuleVendorQuectel)
}
