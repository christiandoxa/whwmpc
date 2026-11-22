package firmware

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
)

const maxMetainfoSize = 64 << 10

var errRawMetainfo = errors.New("ID, GUID, protokol, atau versi metadata CAB tidak cocok dengan rencana")

type metainfoText struct {
	Text     string     `xml:",chardata"`
	Children []struct{} `xml:",any"`
}

type rawMetainfo struct {
	Type     string         `xml:"type,attr"`
	IDs      []metainfoText `xml:"id"`
	Provides []struct {
		Firmware []struct {
			metainfoText
			Type string `xml:"type,attr"`
		} `xml:"firmware"`
	} `xml:"provides"`
	Custom []struct {
		Values []struct {
			metainfoText
			Key string `xml:"key,attr"`
		} `xml:"value"`
	} `xml:"custom"`
	Releases []struct {
		Items []struct {
			Version string `xml:"version,attr"`
		} `xml:"release"`
	} `xml:"releases"`
}

// VerifyRawVersion checks only metainfo extracted from the exact CAB authenticated by PackageMatches.
func VerifyRawVersion(data []byte, expected Device, want Release) error {
	wanted, ok := plannedHexVersion(expected, want)
	if !ok || len(data) == 0 || len(data) > maxMetainfoSize {
		return errRawMetainfo
	}
	meta, err := readRawMetainfo(data)
	if err != nil || meta.Type != "firmware" || len(meta.IDs) != 1 || meta.IDs[0].Text != want.AppstreamID ||
		len(meta.IDs[0].Children) != 0 || len(meta.Provides) != 1 || len(meta.Provides[0].Firmware) != 1 ||
		len(meta.Custom) != 1 || len(meta.Releases) != 1 || len(meta.Releases[0].Items) != 1 {
		return errRawMetainfo
	}
	target := meta.Provides[0].Firmware[0]
	if target.Type != "flashed" || target.Text != expected.GUIDs[0] || len(target.Children) != 0 {
		return errRawMetainfo
	}
	protocols := 0
	for _, value := range meta.Custom[0].Values {
		if value.Key == "LVFS::UpdateProtocol" {
			if value.Text != "org.uefi.capsule" || len(value.Children) != 0 {
				return errRawMetainfo
			}
			protocols++
		}
	}
	actual, ok := rawHexVersion(meta.Releases[0].Items[0].Version)
	if protocols != 1 || !ok || actual != wanted {
		return errRawMetainfo
	}
	return nil
}

func plannedHexVersion(expected Device, want Release) (uint64, bool) {
	if !deviceID.MatchString(expected.ID) || expected.Plugin != "uefi_capsule" || expected.VersionFormat != "hex" ||
		len(expected.GUIDs) != 1 || expected.GUIDs[0] == "" || want.Remote != LVFS || want.AppstreamID == "" ||
		!checksum.MatchString(want.SHA256) || !slices.Contains(want.Flags, "trusted-metadata") {
		return 0, false
	}
	return rawHexVersion(want.Version)
}

func rawHexVersion(version string) (uint64, bool) {
	digits, ok := strings.CutPrefix(version, "0x")
	if !ok || len(digits) > 8 {
		return 0, false
	}
	value, err := strconv.ParseUint(digits, 16, 32)
	return value, err == nil && value > 0
}

func readRawMetainfo(data []byte) (rawMetainfo, error) {
	d := xml.NewTokenDecoder(&metainfoTokens{Decoder: xml.NewDecoder(bytes.NewReader(data))})
	var meta rawMetainfo
	var root, declaration bool
	for {
		token, err := d.Token()
		if err == io.EOF && root {
			return meta, nil
		}
		if err != nil {
			return rawMetainfo{}, errRawMetainfo
		}
		switch node := token.(type) {
		case xml.StartElement:
			if root || node.Name.Local != "component" || d.DecodeElement(&meta, &node) != nil {
				return rawMetainfo{}, errRawMetainfo
			}
			root = true
		case xml.CharData:
			if len(bytes.TrimSpace(node)) != 0 {
				return rawMetainfo{}, errRawMetainfo
			}
		case xml.ProcInst:
			if root || declaration || node.Target != "xml" {
				return rawMetainfo{}, errRawMetainfo
			}
			declaration = true
		case xml.Comment:
		default:
			return rawMetainfo{}, errRawMetainfo
		}
	}
}

type metainfoTokens struct {
	*xml.Decoder
	componentSeen bool
}

func (d *metainfoTokens) Token() (xml.Token, error) {
	token, err := d.Decoder.Token()
	switch node := token.(type) {
	case xml.Directive:
		return nil, errRawMetainfo
	case xml.ProcInst:
		if d.componentSeen {
			return nil, errRawMetainfo
		}
	case xml.StartElement:
		if node.Name.Space != "" {
			return nil, errRawMetainfo
		}
		if node.Name.Local == "component" {
			if d.componentSeen {
				return nil, errRawMetainfo
			}
			d.componentSeen = true
		}
		// The XML decoder otherwise accepts duplicate attributes and matches tags by local name.
		seen := make(map[xml.Name]bool, len(node.Attr))
		for _, attr := range node.Attr {
			if attr.Name.Space != "" || attr.Name.Local == "xmlns" || seen[attr.Name] {
				return nil, errRawMetainfo
			}
			seen[attr.Name] = true
		}
	}
	return token, err
}
