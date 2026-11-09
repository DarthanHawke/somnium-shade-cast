package test

import (
	"bytes"
	"context"
	"encoding/base64"
	"os/exec"
	"testing"
	"time"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/dto"
)

// ordinaryM4A содержит синтетическую синусоиду в M4A с атомом moov после mdat,
// закодированную в Base64.
const ordinaryM4A = `AAAAHGZ0eXBpc29tAAACAGlzb21pc28ybXA0MQAAAAhmcmVlAAADmG1kYXTcAExhdmM2My4xLjEwMgA
CaKNZoSiNNYdKmV9vinEeaXuSJDwiSf/+rkgTdMNzYrT1s210i6u4O2aO/De4sFbKmtMlIlXNtRnMtEpEpEiZKRKBgYGRAwMbNmw
YGRIkRs2bNokSKWWWzcASouI3BEqpyNZNjXCM9tFXSM5dBWyM05KqHAaZKcmia5KWrgE0lNrJXZZLqyXUFOm/386ttauf9vH7+da
41ev/4vX7+da41ev/7F/jzrWtasbU7NuueeYNZbLPcKL3+ygYN0D6UK+lA+n01mA+n0+lAH0+n01gfT6fTXQYAfT6fTWZsAa560W
LVJ/Nt0iEMTPlEz5RM+XyiJg+Xy4xMHy+XEmD5fL5RREAPl8vlxiiAFlz1nAkJCQkJCQkJCQkJCQk5wE28Yr6xk/j+n69cceeHE1
NNXcjfuS1WlQP/DaX3Pj4zNu+/uB8ZgM+gDJ8AZ/cD4zGGf3AP4sA4AEEMYpCXWOe/+P/4v/L/f/N1c1Va37cxmO/XdxuJVCKKJa
9BjRRRRRRdwiiBEhCFRP1rdCCo6icOAEGMYmirRCHUt8/9P/r/t/7f7ypIaVVdZjspSbulQfTduQhRF9N30oZRQaxG3r5TI2sKZb
zSste8JQ2k+ABCDGK+iEOpc8/3/+v8//p/ikutVK65km3bdZG7UofKedJ2NnnnnnPzHE+xR5zzg86gtAMiKcKxyygUqhDOOAA/DG
Rxp0RB1Lvv+//1/n/9P1zikhLy249OJzdZJVBz27d7wT7vT07oHdu3brGQsXQnuaT1dVq01dX9lYnNSmGJiwYC4AA/jGk5Q0Rh04
h0LO/+n9v1/2/nUuFLq6qzxMusiVQmJ0M9Bnr/XuinYKj9fr9frp6aP0agxjm1gVmRrUWBlhUGFbNbZtTg8MxFrMHPpwBMFGZyJ1
Yhbzn/p/v9Jx5rhuJLtHRaSSST8DPPPPPszlCqcM8888885ZSyyyynhDszzzlVw8PPuIAFOPD1uIAGHDw9biABTxl5V60pwE2lbL
JdWS6sldlkrsd/r1xfGta//s9/jzxfHF6//b8/XWtOnU1X9P161eRBtaAyNLYI7loAMZmszNYe+geH0MA8Pp9AHh9PoA88z6fT6A
DX5r+n0+gA2rEIjLAa88xBEohPxHN8hzD5EB8h8uIHy5vl8gPlzD5fLiAPlzT/L5cQBUAuMzGgg+cFpAwIRjNttt8ARiBtHAAAAM
6bW9vdgAAAGxtdmhkAAAAAAAAAAAAAAAAAAC7gAAAJYAAAQAAAQAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAABAAAAAAAAAAA
AAAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAgAAAmV0cmFrAAAAXHRraGQAAAADAAAAAAAAAAAAAAABAAAAAAA
AJYAAAAAAAAAAAAAAAAEBAAAAAAEAAAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAAAAkZWR0cwAAABx
lbHN0AAAAAAAAAAEAACWAAAAEAAABAAAAAAHdbWRpYQAAACBtZGhkAAAAAAAAAAAAAAAAAAC7gAAAKYBVxAAAAAAALWhkbHIAAAA
AAAAAAHNvdW4AAAAAAAAAAAAAAABTb3VuZEhhbmRsZXIAAAABiG1pbmYAAAAQc21oZAAAAAAAAAAAAAAAJGRpbmYAAAAcZHJlZgA
AAAAAAAABAAAADHVybCAAAAABAAABTHN0YmwAAAB+c3RzZAAAAAAAAAABAAAAbm1wNGEAAAAAAAAAAQAAAAAAAAAAAAEAEAAAAAC
7gAAAAAAANmVzZHMAAAAAA4CAgCUAAQAEgICAF0AVAAAAAACAwwAAgMMFgICABRGIVuUABoCAgAECAAAAFGJ0cnQAAAAAAACAwwA
AgMMAAAAgc3R0cwAAAAAAAAACAAAACgAABAAAAAABAAABgAAAABxzdHNjAAAAAAAAAAEAAAABAAAACwAAAAEAAABAc3RzegAAAAA
AAAAAAAAACwAAAIwAAACfAAAAOQAAADkAAAA9AAAAPwAAAEUAAABOAAAATQAAAJIAAAAFAAAAFHN0Y28AAAAAAAAAAQAAACwAAAA
ac2dwZAEAAAByb2xsAAAAAgAAAAH//wAAABxzYmdwAAAAAHJvbGwAAAABAAAACwAAAAEAAABhdWR0YQAAAFltZXRhAAAAAAAAACF
oZGxyAAAAAAAAAABtZGlyYXBwbAAAAAAAAAAAAAAAACxpbHN0AAAAJKl0b28AAAAcZGF0YQAAAAEAAAAATGF2ZjYzLjEuMTAy`

// TestOrdinaryM4AWithMoovAtEndAndHTTPDecode проверяет загрузку M4A с атомом moov в конце
// файла, точность HTTP-выдачи и декодирование потока через ffmpeg.
func TestOrdinaryM4AWithMoovAtEndAndHTTPDecode(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	data, err := base64.StdEncoding.DecodeString(ordinaryM4A)
	if err != nil {
		t.Fatal(err)
	}
	moov := bytes.Index(data, []byte("moov"))
	if moov <= bytes.Index(data, []byte("mdat")) {
		t.Fatal("fixture is not an ordinary M4A with moov at end")
	}
	// Добавляем валидный free-атом перед moov. Смещения mdat не меняются;
	// хвост с метаданными становится достаточно далёким для seek.
	free := make([]byte, 512*1024)
	free[1] = 8
	copy(free[4:8], "free")
	at := moov - 4
	padded := make([]byte, 0, len(data)+len(free))
	padded = append(padded, data[:at]...)
	padded = append(padded, free...)
	padded = append(padded, data[at:]...)
	h := newRig(t)
	owner := h.user("owner")
	uploaded := decode[dto.TrackResponse](t, responseBody(
		t, h.multipart(owner.PublicID, `{}`, padded, "ordinary.m4a"), 201))
	path := "/stream/" + uploaded.PublicID
	result := h.request("GET", path, owner.PublicID, nil, nil)
	if result.Header.Get("Content-Type") != "audio/mp4" || !bytes.Equal(responseBody(t, result, 200), padded) {
		t.Fatal("ordinary M4A headers or bytes differ")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(
		ctx, ffmpeg, "-v", "error", "-http_proxy", "", "-headers",
		"X-User: "+string(owner.PublicID)+"\r\n", "-i", h.http.URL+path, "-f", "null", "-")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("HTTP M4A decode failed: %v: %s", err, out)
	}
}
