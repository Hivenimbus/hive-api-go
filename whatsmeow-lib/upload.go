// Copyright (c) 2024 Tulir Asokan
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package whatsmeow

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"go.mau.fi/util/random"

	"go.mau.fi/whatsmeow/socket"
	"go.mau.fi/whatsmeow/util/cbcutil"
)

// UploadResponse contains the data from the attachment upload, which can be put into a message to send the attachment.
type UploadResponse struct {
	URL        string `json:"url"`
	DirectPath string `json:"direct_path"`
	Handle     string `json:"handle"`
	ObjectID   string `json:"object_id"`

	MediaKey      []byte `json:"-"`
	FileEncSHA256 []byte `json:"-"`
	FileSHA256    []byte `json:"-"`
	FileLength    uint64 `json:"-"`
}

// Upload uploads the given attachment to WhatsApp servers.
//
// You should copy the fields in the response to the corresponding fields in a protobuf message.
//
// For example, to send an image:
//
//	resp, err := cli.Upload(context.Background(), yourImageBytes, whatsmeow.MediaImage)
//	// handle error
//
//	imageMsg := &waE2E.ImageMessage{
//		Caption:  proto.String("Hello, world!"),
//		Mimetype: proto.String("image/png"), // replace this with the actual mime type
//		// you can also optionally add other fields like ContextInfo and JpegThumbnail here
//
//		URL:           &resp.URL,
//		DirectPath:    &resp.DirectPath,
//		MediaKey:      resp.MediaKey,
//		FileEncSHA256: resp.FileEncSHA256,
//		FileSHA256:    resp.FileSHA256,
//		FileLength:    &resp.FileLength,
//	}
//	_, err = cli.SendMessage(context.Background(), targetJID, &waE2E.Message{
//		ImageMessage: imageMsg,
//	})
//	// handle error again
//
// The same applies to the other message types like DocumentMessage, just replace the struct type and Message field name.
func (cli *Client) Upload(ctx context.Context, plaintext []byte, appInfo MediaType) (resp UploadResponse, err error) {
	resp.FileLength = uint64(len(plaintext))
	resp.MediaKey = random.Bytes(32)

	plaintextSHA256 := sha256.Sum256(plaintext)
	resp.FileSHA256 = plaintextSHA256[:]

	iv, cipherKey, macKey, _ := getMediaKeys(resp.MediaKey, appInfo)

	var ciphertext []byte
	ciphertext, err = cbcutil.Encrypt(cipherKey, iv, plaintext)
	if err != nil {
		err = fmt.Errorf("failed to encrypt file: %w", err)
		return
	}

	h := hmac.New(sha256.New, macKey)
	h.Write(iv)
	h.Write(ciphertext)
	dataToUpload := append(ciphertext, h.Sum(nil)[:10]...)

	dataHash := sha256.Sum256(dataToUpload)
	resp.FileEncSHA256 = dataHash[:]

	err = cli.rawUpload(ctx, bytes.NewReader(dataToUpload), uint64(len(dataToUpload)), resp.FileEncSHA256, appInfo, false, &resp)
	return
}

// UploadReader uploads the given attachment to WhatsApp servers.
//
// This is otherwise identical to [Upload], but it reads the plaintext from an [io.Reader] instead of a byte slice.
// A temporary file is required for the encryption process. If tempFile is nil, a temporary file will be created
// and deleted after the upload.
//
// To use only one file, pass the same file as both plaintext and tempFile. This will cause the file to be overwritten with encrypted data.
func (cli *Client) UploadReader(ctx context.Context, plaintext io.Reader, tempFile io.ReadWriteSeeker, appInfo MediaType) (resp UploadResponse, err error) {
	resp.MediaKey = random.Bytes(32)
	iv, cipherKey, macKey, _ := getMediaKeys(resp.MediaKey, appInfo)
	if tempFile == nil {
		tempFile, err = os.CreateTemp("", "whatsmeow-upload-*")
		if err != nil {
			err = fmt.Errorf("failed to create temporary file: %w", err)
			return
		}
		defer func() {
			tempFileFile := tempFile.(*os.File)
			_ = tempFileFile.Close()
			_ = os.Remove(tempFileFile.Name())
		}()
	}
	var uploadSize uint64
	resp.FileSHA256, resp.FileEncSHA256, resp.FileLength, uploadSize, err = cbcutil.EncryptStream(cipherKey, iv, macKey, plaintext, tempFile)
	if err != nil {
		err = fmt.Errorf("failed to encrypt file: %w", err)
		return
	}
	_, err = tempFile.Seek(0, io.SeekStart)
	if err != nil {
		err = fmt.Errorf("failed to seek to start of temporary file: %w", err)
		return
	}
	err = cli.rawUpload(ctx, tempFile, uploadSize, resp.FileEncSHA256, appInfo, false, &resp)
	return
}

// UploadNewsletter uploads the given attachment to WhatsApp servers without encrypting it first.
//
// Newsletter media works mostly the same way as normal media, with a few differences:
// * Since it's unencrypted, there's no MediaKey or FileEncSHA256 fields.
// * There's a "media handle" that needs to be passed in SendRequestExtra.
//
// Example:
//
//	resp, err := cli.UploadNewsletter(context.Background(), yourImageBytes, whatsmeow.MediaImage)
//	// handle error
//
//	imageMsg := &waE2E.ImageMessage{
//		// Caption, mime type and other such fields work like normal
//		Caption:  proto.String("Hello, world!"),
//		Mimetype: proto.String("image/png"),
//
//		// URL and direct path are also there like normal media
//		URL:        &resp.URL,
//		DirectPath: &resp.DirectPath,
//		FileSHA256: resp.FileSHA256,
//		FileLength: &resp.FileLength,
//		// Newsletter media isn't encrypted, so the media key and file enc sha fields are not applicable
//	}
//	_, err = cli.SendMessage(context.Background(), newsletterJID, &waE2E.Message{
//		ImageMessage: imageMsg,
//	}, whatsmeow.SendRequestExtra{
//		// Unlike normal media, newsletters also include a "media handle" in the send request.
//		MediaHandle: resp.Handle,
//	})
//	// handle error again
func (cli *Client) UploadNewsletter(ctx context.Context, data []byte, appInfo MediaType) (resp UploadResponse, err error) {
	resp.FileLength = uint64(len(data))
	hash := sha256.Sum256(data)
	resp.FileSHA256 = hash[:]
	err = cli.rawUpload(ctx, bytes.NewReader(data), resp.FileLength, resp.FileSHA256, appInfo, true, &resp)
	return
}

// UploadNewsletterReader uploads the given attachment to WhatsApp servers without encrypting it first.
//
// This is otherwise identical to [UploadNewsletter], but it reads the plaintext from an [io.Reader] instead of a byte slice.
// Unlike [UploadReader], this does not require a temporary file. However, the data needs to be hashed first,
// so an [io.ReadSeeker] is required to be able to read the data twice.
func (cli *Client) UploadNewsletterReader(ctx context.Context, data io.ReadSeeker, appInfo MediaType) (resp UploadResponse, err error) {
	hasher := sha256.New()
	var fileLength int64
	fileLength, err = io.Copy(hasher, data)
	resp.FileLength = uint64(fileLength)
	resp.FileSHA256 = hasher.Sum(nil)
	_, err = data.Seek(0, io.SeekStart)
	if err != nil {
		err = fmt.Errorf("failed to seek to start of data: %w", err)
		return
	}
	err = cli.rawUpload(ctx, data, resp.FileLength, resp.FileSHA256, appInfo, true, &resp)
	return
}

func (cli *Client) rawUpload(ctx context.Context, dataToUpload io.Reader, uploadSize uint64, fileHash []byte, appInfo MediaType, newsletter bool, resp *UploadResponse) error {
	token := base64.URLEncoding.EncodeToString(fileHash)
	mmsType := mediaTypeToMMSType[appInfo]
	uploadPrefix := "mms"
	if cli.MessengerConfig != nil {
		uploadPrefix = "wa-msgr/mms"
		if mmsType == "audio" {
			mmsType = "ptt"
		}
	}
	if newsletter {
		mmsType = fmt.Sprintf("newsletter-%s", mmsType)
		uploadPrefix = "newsletter"
	}

	// isTransientUploadError returns true for TCP-level errors that are safe to retry.
	isTransientUploadError := func(e error) bool {
		if e == nil {
			return false
		}
		msg := e.Error()
		return strings.Contains(msg, "connection reset by peer") ||
			strings.Contains(msg, "broken pipe") ||
			strings.Contains(msg, "EOF") ||
			strings.Contains(msg, "failed to execute request") ||
			strings.Contains(msg, "failed to refresh media connections")
	}

	// tryUpload attempts the upload to a specific host using the given mediaConn auth.
	// freshTransport forces a new TCP connection instead of reusing pooled ones.
	tryUpload := func(host string, auth string, freshTransport bool) error {
		q := url.Values{
			"auth":  []string{auth},
			"token": []string{token},
		}
		uploadURL := url.URL{
			Scheme:   "https",
			Host:     host,
			Path:     fmt.Sprintf("/%s/%s/%s", uploadPrefix, mmsType, token),
			RawQuery: q.Encode(),
		}

		// When a previous attempt failed with a TCP-level reset, we read the reader
		// so we need to seek back to start if it supports seeking.
		if seeker, ok := dataToUpload.(io.Seeker); ok {
			_, _ = seeker.Seek(0, io.SeekStart)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL.String(), dataToUpload)
		if err != nil {
			return fmt.Errorf("failed to prepare request: %w", err)
		}
		req.ContentLength = int64(uploadSize)
		req.Header.Set("Origin", socket.Origin)
		req.Header.Set("Referer", socket.Origin+"/")

		httpClient := cli.mediaHTTP
		if freshTransport {
			// Use a fresh HTTP client with no keep-alive to avoid broken TCP connections.
			httpClient = &http.Client{
				Transport: &http.Transport{
					DisableKeepAlives: true,
				},
				Timeout: 120 * time.Second,
			}
		}

		httpResp, err := httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to execute request: %w", err)
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			return fmt.Errorf("upload failed with status code %d", httpResp.StatusCode)
		}
		if err = json.NewDecoder(httpResp.Body).Decode(resp); err != nil {
			return fmt.Errorf("failed to parse upload response: %w", err)
		}
		return nil
	}

	const maxRounds = 3
	for round := 1; round <= maxRounds; round++ {
		// Force-refresh media connections on every round so we get a fresh auth token
		// and potentially different hosts after a failure.
		forceRefresh := round > 1
		mediaConn, err := cli.refreshMediaConn(ctx, forceRefresh)
		if err != nil {
			if round == maxRounds {
				return fmt.Errorf("failed to refresh media connections: %w", err)
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// Build the host list to try this round.
		var hosts []string
		if cli.MessengerConfig != nil {
			// Messenger: prefer last host.
			hosts = []string{mediaConn.Hosts[len(mediaConn.Hosts)-1].Hostname}
		} else {
			for _, h := range mediaConn.Hosts {
				hosts = append(hosts, h.Hostname)
			}
		}

		freshTransport := round > 1
		var lastErr error
		for _, host := range hosts {
			lastErr = tryUpload(host, mediaConn.Auth, freshTransport)
			if lastErr == nil {
				return nil
			}
			if !isTransientUploadError(lastErr) {
				// Non-transient error (e.g. 403, bad token) — no point trying other hosts.
				return lastErr
			}
			// Transient: try the next host.
		}

		// All hosts in this round failed.
		if round < maxRounds {
			time.Sleep(500 * time.Millisecond)
		} else {
			return fmt.Errorf("upload failed after %d rounds across all CDN hosts: %w", maxRounds, lastErr)
		}
	}

	return fmt.Errorf("upload failed after %d rounds", maxRounds)
}
