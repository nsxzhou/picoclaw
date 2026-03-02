package agent

import (
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/providers"
)

func toFileRefMeta(fileRefs []bus.FileRef) []providers.FileRefMeta {
	if len(fileRefs) == 0 {
		return nil
	}

	metas := make([]providers.FileRefMeta, len(fileRefs))
	for i, ref := range fileRefs {
		metas[i] = providers.FileRefMeta{
			Name:            ref.Name,
			MediaType:       ref.MediaType,
			Kind:            string(ref.Kind),
			Source:          string(ref.Source),
			FeishuMessageID: ref.FeishuMessageID,
			FeishuFileKey:   ref.FeishuFileKey,
			FeishuResType:   ref.FeishuResType,
		}
	}
	return metas
}

func toBusFileRefs(metas []providers.FileRefMeta) []bus.FileRef {
	if len(metas) == 0 {
		return nil
	}

	refs := make([]bus.FileRef, len(metas))
	for i, meta := range metas {
		refs[i] = bus.FileRef{
			Name:            meta.Name,
			MediaType:       meta.MediaType,
			Kind:            bus.AttachmentKind(meta.Kind),
			Source:          bus.FileRefSource(meta.Source),
			FeishuMessageID: meta.FeishuMessageID,
			FeishuFileKey:   meta.FeishuFileKey,
			FeishuResType:   meta.FeishuResType,
		}
	}
	return refs
}
