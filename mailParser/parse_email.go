package main

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"strings"

	"github.com/gabriel-vasile/mimetype"
)


const (
	contentTypeMultipartMixed        = "multipart/mixed"
	contentTypeMultipartAlternative  = "multipart/alternative"
	contentTypeMultipartRelated      = "multipart/related"
	contentTypeTextHtml              = "text/html"
	contentTypeTextPlain             = "text/plain"
	contentTypeRfc822                = "message/rfc822"
	contentTypeTextXAmpHtml          = "text/x-amp-html"
	contentTypeTextCalendar          = "text/calendar"
	contentTypeMessageDeliveryStatus = "message/delivery-status"
	contentTypePgpSignature          = "application/pgp-signature"
	contentTypeMultipartSigned       = "multipart/signed"
)


func fixContentType(contentType string) string {
	var fixedContentType string
	splits := strings.Split(contentType, ";")
	for i, split := range splits {
		if i == 0 {
		} else if split == "" {
			continue
		} else if splits[i-1] == split {
			continue
		}
		fixedContentType += split + ";"
	}
	fixedContentType = strings.Replace(fixedContentType, ";;", ";", -1)
	if !strings.HasSuffix(contentType, ";") && strings.HasSuffix(fixedContentType, ";") {
		fixedContentType = strings.TrimSuffix(fixedContentType, ";")
	}
	return fixedContentType
}


func parseMessageIdList(s string) (result []string) {
	for _, p := range strings.Split(s, " ") {
		if strings.Trim(p, " \n") != "" {
			result = append(result, p)
		}
	}
	return
}


func isEmbeddedFile(part *multipart.Part) bool {
	return part.Header.Get("Content-Transfer-Encoding") != "" && part.Header.Get("Content-Id") != ""
}


func decodeMimeSentence(s string) string {
	result := []string{}
	ss := strings.Split(s, " ")

	for _, word := range ss {
		dec := new(mime.WordDecoder)
		w, err := dec.Decode(word)
		if err != nil {
			if len(result) == 0 {
				w = word
			} else {
				w = " " + word
			}
		}

		result = append(result, w)
	}
	return strings.Join(result, "")
}


func decodeContent(content io.Reader, contentTransferEncoding string) (io.Reader, error) {
	switch strings.ToLower(contentTransferEncoding) {
	case "base64":
		decoded := base64.NewDecoder(base64.StdEncoding, content)
		b, _ := io.ReadAll(decoded)
		return bytes.NewReader(b), nil
	case "7bit", "8bit", "8bits", "binary":
		dd, err := io.ReadAll(content)
		return bytes.NewReader(dd), err
	case "", "quoted-printable":
		return content, nil
	default:
		return content, fmt.Errorf("unknown encoding: %s", contentTransferEncoding)
	}
}

func hashBytes(data []byte, salt []byte) string {
	data = append(data, salt...)
	sha1sumtext := sha1.Sum(data)
	return hex.EncodeToString(sha1sumtext[:])
}

func decodeFilePart(part *multipart.Part) (decodedFile FileDecode, err error) {
	ddata, err := decodeContent(part, part.Header.Get("Content-Transfer-Encoding"))
	if err != nil {
		return decodedFile, err
	}
	decodedFile.Data, _ = io.ReadAll(ddata)
	mtype := mimetype.Detect(decodedFile.Data)
	decodedFile.Hash = hashBytes(decodedFile.Data, nil)
	if err == nil {
		decodedFile.RealMimetype = mtype.String()
		decodedFile.RealExtension = mtype.Extension()
		//: mimtype per default reads only first 3072 bytes to get mimetype.
		//: Sometimes it is not sufficient, so mimetype wont return a extensionstring.
		//: mimteype.SetLimit(0) will force mimetpye to read the full file.
		if decodedFile.RealExtension == "" {
			mimetype.SetLimit(0)
			mtype = mimetype.Detect(decodedFile.Data)
			decodedFile.RealMimetype = mtype.String()
			decodedFile.RealExtension = mtype.Extension()
			mimetype.SetLimit(3072)
		}
	}

	decodedFile.ContentType = strings.Split(fixContentType(part.Header.Get("Content-Type")), ";")[0]
	decodedFile.ContentDisposition = strings.Split(part.Header.Get("Content-Disposition"), ";")[0]
	decodedFile.ContentTransferEncoding = part.Header.Get("Content-Transfer-Encoding")
	return decodedFile, err
}


func decodeEmbeddedFile(part *multipart.Part) (ef AttachmentEmbedding, err error) {
	cid := decodeMimeSentence(part.Header.Get("Content-Id"))
	decodedEmbeddedFile, err := decodeFilePart(part)
	if err != nil {
		return
	}
	ef.Filename = strings.Trim(cid, "<>")
	ef.Details = decodedEmbeddedFile
	ef.IsEmbedding = 1
	return
}


func isAttachment(part *multipart.Part) bool {
	return part.FileName() != "" || strings.HasPrefix(part.Header.Get("Content-Disposition"), "attachment")
}


func decodeAttachment(part *multipart.Part) (at AttachmentEmbedding, err error) {
	decodedAttachment, err := decodeFilePart(part)
	if err != nil {
		return
	}
	at.Filename = decodeMimeSentence(part.FileName())
	at.Details = decodedAttachment
	at.IsAttachment = 1
	return
}


func partParser(msg io.Reader, boundary string) (
	TextBody string, HTMLBody string, BodyParts []BodyPart, AttachmentsEmbeddings []AttachmentEmbedding, err error) {

	tp := textproto.NewReader(bufio.NewReader(msg))//bytes.NewReader(body)))
	mpr := multipart.NewReader(tp.R, boundary)
	
	var errcounter uint8 = 0
	for partCounter := 0; ; partCounter++ {
		part, err := mpr.NextPart()
		if err != nil {
			if errcounter == 5 {
				break
			}
			if err == io.EOF {
				break
			} else if err.Error() == "multipart: NextPart: EOF" {
				break
			} else if err != nil {
				errcounter += 1
				continue
			}
		}

		var bodyPart BodyPart
		bodyPart.Id = partCounter
		bodyPart.Filename = decodeMimeSentence(part.FileName())
		bodyPart.FormName = part.FormName()
		//bodyPart.ContentType = strings.Split(part.Header.Get("Content-Type"), ";")[0]
		//bodyPart.ContentType = fixContentType(part.Header.Get("Content-Type"))
		bodyPart.ContentTransferEncoding = part.Header.Get("Content-Transfer-Encoding")
		bodyPart.ContentDisposition = strings.Split(part.Header.Get("Content-Disposition"), ";")[0]
		contentType, params, err := mime.ParseMediaType(fixContentType(part.Header.Get("Content-Type")))
		if err != nil {
			Logger.Info().Err(err).Msg("Cant parse Content-Type from email.")
		}
		bodyPart.ContentType = contentType
		BodyParts = append(BodyParts, bodyPart)

		if bodyPart.ContentType == contentTypeMultipartMixed ||
			bodyPart.ContentType == contentTypeMultipartAlternative ||
			bodyPart.ContentType == contentTypeMultipartRelated {

			tb, hb, _, ef, err := partParser(part, params["boundary"])
			if err != nil {
				Logger.Info().Err(err).Msg("Cant decode Body-Content.")
			}
			HTMLBody += hb
			TextBody += tb
			AttachmentsEmbeddings = append(AttachmentsEmbeddings, ef...)

		} else if isAttachment(part) {
			at, err := decodeAttachment(part)
			if err != nil {
				Logger.Info().Err(err).Msg("Malforemd attachment found.")
				continue
			}
			AttachmentsEmbeddings = append(AttachmentsEmbeddings, at)

		} else if isEmbeddedFile(part) {
			ef, err := decodeEmbeddedFile(part)
			if err != nil {
				Logger.Info().Err(err).Msg("Malformed embeding found.")
			}
			AttachmentsEmbeddings = append(AttachmentsEmbeddings, ef)
		}
	}
	return
}


func readData(data []byte) (mail.Message, error) {
	//: Check if data is .msg file format
	//: Ceate 5 bytes buffer to check if file is .msg file (Starting with "From ")
	msg_starting_pattern := []byte{70, 114, 111, 109, 32}
	path_starting_pattern := []byte{80, 97, 116, 104, 58}

	if bytes.HasPrefix(data, msg_starting_pattern) ||
		bytes.HasPrefix(data, path_starting_pattern) {
		Logger.Warn().Msg("msg file format detected, parsing not fully supported")
	}

	eml := textproto.NewReader(bufio.NewReader(bytes.NewReader(data)))
	mail_header, err := eml.ReadMIMEHeader()

	return mail.Message{
		Header: mail.Header(mail_header),
		Body:   eml.R,
	}, err
}


func ParseEmail(Eua *EmailUnderAnalysis, raw []byte) error {

	msg, err := readData(raw)
	if err != nil {
		Logger.Info().Err(err).Msg("Coundt parse email header")
	}

	var params map[string]string
	if msg.Header.Get("Content-Type") == "" {
		Eua.ContentType = "text/plain"
	} else {
		Eua.ContentType, params, err = mime.ParseMediaType(fixContentType(msg.Header.Get("Content-Type")))
		if err != nil {
			Logger.Info().Err(err).Msg("Couldnt parse ContentType")
			return err
		}
	}

	switch Eua.ContentType {

	//this is the default case
	case contentTypeMultipartMixed, contentTypeMultipartAlternative, contentTypeMultipartRelated:
		Eua.TextBody, Eua.HTMLBody, Eua.BodyParts, Eua.AttachmentsEmbeddings, err = partParser(msg.Body, params["boundary"])

	case contentTypeTextHtml:
		data, _ := decodeContent(msg.Body, msg.Header.Get("Content-Transfer-Encoding"))
		//to-do: add error handling
		message, _ := io.ReadAll(data)
		Eua.HTMLBody = strings.TrimSuffix(string(message[:]), "\n")

	case contentTypeTextPlain,
		contentTypeTextCalendar,
		contentTypeMessageDeliveryStatus,
		contentTypeRfc822,
		contentTypePgpSignature,
		contentTypeMultipartSigned:
		data, _ := decodeContent(msg.Body, msg.Header.Get("Content-Transfer-Encoding"))
		//to-do: add error handling
		message, _ := io.ReadAll(data)
		Eua.TextBody = strings.TrimSuffix(string(message[:]), "\n")

	default:
		message, _ := io.ReadAll(msg.Body)
		Eua.TextBody = strings.TrimSuffix(string(message[:]), "\n")
		Eua.HTMLBody = strings.TrimSuffix(string(message[:]), "\n")
	}

	return err
}
