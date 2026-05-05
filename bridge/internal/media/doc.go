// Package media bridges negotiated SIP media to the local bridge protocol.
//
// The plugin-facing contract is intentionally narrower than RTP: it only emits
// and accepts canonical 20 ms PCMU frames. The POC SIP offer is PCMU-only so we
// can validate the end-to-end voice path without debugging transcoding.
package media
