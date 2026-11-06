package main

// wallpaper.go -- v0.63 (detail-fix ②): 设为壁纸 from an imported
// file. POST /api/wallpaper/fromfile {path, fit} copies one
// Data/files image into the custom-wallpaper store and persists
// the chosen fit mode in one step.
//
// The copy semantics are the personalization upload's exactly:
// the bytes land in Data/wallpaper.bin (+ wallpaper.ctype), so
// the wallpaper survives the source file being renamed, moved
// or deleted, and travels with the portable folder. The fit
// mode is the plain settings key wallpaperFit -- the dialog,
// 设置 → 外观 and the desktop's applyWallpaper all read the one
// value, and persisting it through the settings store emits
// the usual "settings" bus event, so every open surface
// re-applies live.
//
// Refusals (bad fit, path outside Data/files, not an image, an
// image the stdlib decoders cannot read) change NOTHING: the
// previous wallpaper and fit stay exactly as they were, and the
// failure is logged under [wallpaper] with the file's name.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// wallpaperMaxBytes matches POST /api/wallpaper's upload cap.
const wallpaperMaxBytes = 25 << 20

type wallpaperFromFileRequest struct {
	Path string `json:"path"` // Data/files-relative, as the file items carry it
	Fit  string `json:"fit"`  // fill | fit | center | stretch
}

func wallpaperFromFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, errCodeMethod, "method not allowed")
		return
	}
	var body wallpaperFromFileRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiErrBadRequest(w, errCodeBadRequest, "请求格式不对")
		return
	}
	if !validWallpaperFit(body.Fit) {
		logf("wallpaper", "warn", "设为壁纸失败：%s：适配方式不对（%q）", body.Path, body.Fit)
		apiErrBadRequest(w, errCodeInvalid, "适配方式不对")
		return
	}
	abs, err := filesResolve(body.Path)
	if err != nil {
		logf("wallpaper", "warn", "设为壁纸失败：%s：%v", body.Path, err)
		apiErrBadRequest(w, errCodeBadRequest, err.Error())
		return
	}
	name := filepath.Base(abs)
	b, ct, err := wallpaperImageBytes(abs)
	if err != nil {
		logf("wallpaper", "warn", "设为壁纸失败：%s：%v", name, err)
		apiErrBadRequest(w, errCodeInvalid, err.Error())
		return
	}
	if err := os.WriteFile(wallpaperFile(), b, 0o644); err != nil {
		logf("wallpaper", "error", "设为壁纸失败：%s：壁纸保存失败：%v", name, err)
		writeAPIError(w, http.StatusInternalServerError, errCodeInternal, "壁纸保存失败："+err.Error())
		return
	}
	_ = os.WriteFile(wallpaperTypeFile(), []byte(ct), 0o644)
	persistWallpaperFit(body.Fit)
	logf("wallpaper", "info", "设为壁纸：%s（适配：%s）", name, wallpaperFitLabel(body.Fit))
	writeJSON(w, okResponse{OK: true})
}

// wallpaperImageBytes validates one resolved Data/files entry
// as a wallpaper source and returns its bytes + content type:
// the entry must exist, be a file whose extension the product
// treats as an image (imageExts), fit the wallpaper size cap,
// and -- for the formats the stdlib decoders cover (thumbExts:
// jpg/png/gif) -- actually decode. webp/bmp have no stdlib
// decoder; the dialog's own <img> probe has already verified
// those render before this endpoint is ever called.
func wallpaperImageBytes(abs string) ([]byte, string, error) {
	fi, err := os.Stat(abs)
	if err != nil || fi.IsDir() {
		return nil, "", fmt.Errorf("文件不存在")
	}
	ext := strings.ToLower(filepath.Ext(abs))
	if !imageExts[ext] {
		return nil, "", fmt.Errorf("不是图片文件")
	}
	if fi.Size() > wallpaperMaxBytes {
		return nil, "", fmt.Errorf("图片太大（上限 25MB）")
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", fmt.Errorf("图片读不出来")
	}
	if thumbExts[ext] {
		if _, _, err := image.DecodeConfig(bytes.NewReader(b)); err != nil {
			return nil, "", fmt.Errorf("图片读不出来（文件可能已损坏）")
		}
	}
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		ct = "image/jpeg"
	}
	return b, ct, nil
}

// persistWallpaperFit writes the fit mode through the settings
// store (its save hook emits the "settings" bus event, so open
// surfaces re-apply the wallpaper live). A no-change write is
// skipped; a save failure restores the in-memory value and is
// logged -- the set itself already landed in wallpaper.bin.
func persistWallpaperFit(fit string) {
	setState.Lock()
	defer setState.Unlock()
	settingsLoadLocked()
	old := setState.cur.WallpaperFit
	if old == fit {
		return
	}
	setState.cur.WallpaperFit = fit
	if err := settingsSaveLocked(); err != nil {
		setState.cur.WallpaperFit = old
		logf("wallpaper", "error", "壁纸适配保存失败：%v", err)
	}
}
