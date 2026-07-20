package credentials

import (
	"code.linenisgreat.com/tommy/pkg/cst"
	"code.linenisgreat.com/tommy/pkg/document"
)

// DecodeCommandConfigInto populates a CommandConfig from the [credentials]
// table. Generated config code delegates here for the *CommandConfig field,
// passing the table's cst.Value model node (tommy 0.4.x decode contract).
func DecodeCommandConfigInto(data *CommandConfig, sub *cst.Value) error {
	if v, ok := sub.Get("read"); ok && v.Kind == cst.VLeaf {
		if s, sok := cst.ExtractString(v.Leaf); sok {
			data.Read = s
			v.MarkConsumed()
		}
	}
	if v, ok := sub.Get("write"); ok && v.Kind == cst.VLeaf {
		if s, sok := cst.ExtractString(v.Leaf); sok {
			data.Write = s
			v.MarkConsumed()
		}
	}
	if v, ok := sub.Get("delete"); ok && v.Kind == cst.VLeaf {
		if s, sok := cst.ExtractString(v.Leaf); sok {
			data.Delete = s
			v.MarkConsumed()
		}
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
