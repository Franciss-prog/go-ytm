package ytmusic

import "regexp"

var channelIDRE = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)

// parseAccount reads account/account_menu. ok is false when the response
// has no active account header, i.e. the request was not signed in.
//
// Paths from ytmusicapi mixins/library.py get_account_info
// (sigma67/ytmusicapi@86fcd17); the channel-id rule mirrors ytm-player
// services/auth.py _channel_id_from_account_menu: exactly one ACCOUNT_BOX
// link to a UC… id of page type MUSIC_PAGE_TYPE_USER_CHANNEL, else empty.
func parseAccount(resp node) (AccountInfo, bool) {
	menu := resp.get("actions", 0, "openPopupAction", "popup", "multiPageMenuRenderer")
	hdr := menu.get("header", "activeAccountHeaderRenderer")
	if !hdr.exists() {
		return AccountInfo{}, false
	}
	info := AccountInfo{
		Name:     hdr.get("accountName").runsText(),
		Handle:   hdr.get("channelHandle").runsText(),
		PhotoURL: hdr.get("accountPhoto", "thumbnails", 0, "url").str(),
	}
	if info.Name == "" {
		return AccountInfo{}, false
	}
	var found []string
	for _, sec := range menu.get("sections").list() {
		for _, it := range sec.get("multiPageMenuSectionRenderer", "items").list() {
			link := it.get("compactLinkRenderer")
			if link.get("icon", "iconType").str() != "ACCOUNT_BOX" {
				continue
			}
			b := link.get("navigationEndpoint", "browseEndpoint")
			id := b.get("browseId").str()
			if channelIDRE.MatchString(id) && pageType(b) == pageUserChannel {
				found = append(found, id)
			}
		}
		break // the active account's own section only
	}
	if len(found) == 1 {
		info.ChannelID = found[0]
	}
	return info, true
}
