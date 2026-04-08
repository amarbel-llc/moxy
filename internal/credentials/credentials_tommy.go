package credentials

import (
	"github.com/amarbel-llc/tommy/pkg/cst"
	"github.com/amarbel-llc/tommy/pkg/document"
)

// DecodeCommandConfigInto populates a CommandConfig from a TOML table node.
// Generated config code calls this for the [credentials] table.
func DecodeCommandConfigInto(data *CommandConfig, doc *document.Document, container *cst.Node, consumed map[string]bool, keyPrefix string) error {
	if v, err := document.GetFromContainer[string](doc, container, "read"); err == nil {
		data.Read = v
		consumed[keyPrefix+"read"] = true
	}
	if v, err := document.GetFromContainer[string](doc, container, "write"); err == nil {
		data.Write = v
		consumed[keyPrefix+"write"] = true
	}
	if v, err := document.GetFromContainer[string](doc, container, "delete"); err == nil {
		data.Delete = v
		consumed[keyPrefix+"delete"] = true
	}
	return nil
}

// EncodeCommandConfigFrom writes a CommandConfig into a TOML table node.
func EncodeCommandConfigFrom(data *CommandConfig, doc *document.Document, container *cst.Node) error {
	if data.Read != "" || doc.HasInContainer(container, "read") {
		if err := doc.SetInContainer(container, "read", data.Read); err != nil {
			return err
		}
	}
	if data.Write != "" || doc.HasInContainer(container, "write") {
		if err := doc.SetInContainer(container, "write", data.Write); err != nil {
			return err
		}
	}
	if data.Delete != "" || doc.HasInContainer(container, "delete") {
		if err := doc.SetInContainer(container, "delete", data.Delete); err != nil {
			return err
		}
	}
	return nil
}
