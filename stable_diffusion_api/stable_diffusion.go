package stable_diffusion_api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"regexp"
)

type apiImpl struct {
	host string
}

type Config struct {
	Host string
}

func New(cfg Config) (StableDiffusionAPI, error) {
	if cfg.Host == "" {
		return nil, errors.New("missing host")
	}

	// remove trailing slash
	if cfg.Host[len(cfg.Host)-1:] == "/" {
		cfg.Host = cfg.Host[:len(cfg.Host)-1]
	}

	return &apiImpl{
		host: cfg.Host,
	}, nil
}

type jsonTextToImageResponse struct {
	Images []string `json:"images"`
	Info   string   `json:"info"`
}

type jsonInfoResponse struct {
	Seed        int   `json:"seed"`
	AllSeeds    []int `json:"all_seeds"`
	AllSubseeds []int `json:"all_subseeds"`
}

type TextToImageResponse struct {
	Images   []string `json:"images"`
	Seeds    []int    `json:"seeds"`
	Subseeds []int    `json:"subseeds"`
	Model    string   `json:"model"`
}

type Txt2ImgOverrideSettings struct {
	// png, jpg, webp
	GridFormat string `json:"grid_format,omitempty"`
	ReturnGrid *bool  `json:"return_grid,omitempty"`
	// png, jpg, webp
	SamplesFormat string `json:"samples_format,omitempty"`
	// new option since 04/29/2023 https://github.com/AUTOMATIC1111/stable-diffusion-webui/pull/9177
	NegativeGuidanceMinimumSigma float32 `json:"s_min_uncond,omitempty"`

	// this is in blacklist. See stable-diffusion-webui/modules/shared.py:124:restricted_opts
	OutdirTxt2ImgSamples string `json:"outdir_txt2img_samples,omitempty"`
}

type TextToImageRequest struct {
	Prompt            string  `json:"prompt"`
	NegativePrompt    string  `json:"negative_prompt"`
	Width             int     `json:"width"`
	Height            int     `json:"height"`
	RestoreFaces      bool    `json:"restore_faces"`
	EnableHR          bool    `json:"enable_hr"`
	HrScale           float32 `json:"hr_scale,omitempty"`
	HrUpscaler        string  `json:"hr_upscaler,omitempty"`
	HrSecondPassSteps int     `json:"hr_second_pass_steps,omitempty"`
	HRResizeX         int     `json:"hr_resize_x"`
	HRResizeY         int     `json:"hr_resize_y"`
	DenoisingStrength float64 `json:"denoising_strength"`
	BatchSize         int     `json:"batch_size"`
	Seed              int     `json:"seed"`
	Subseed           int     `json:"subseed"`
	SubseedStrength   float64 `json:"subseed_strength"`
	SamplerName       string  `json:"sampler_name"`
	CfgScale          float64 `json:"cfg_scale"`
	Steps             int     `json:"steps"`
	NIter             int     `json:"n_iter"`

	// Save sample images AND grid copies to output dir
	SaveImages       bool                    `json:"save_images"`
	OverrideSettings Txt2ImgOverrideSettings `json:"override_settings"`
}

func (api *apiImpl) TextToImage(req *TextToImageRequest) (*TextToImageResponse, error) {
	if req == nil {
		return nil, errors.New("missing request")
	}

	postURL := api.host + "/sdapi/v1/txt2img"

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequest("POST", postURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	request.Header.Set("Content-Type", "application/json; charset=UTF-8")

	client := &http.Client{}

	response, err := client.Do(request)
	if err != nil {
		log.Printf("API URL: %s", postURL)
		log.Printf("Error with API Request: %s", string(jsonData))

		return nil, err
	}

	defer response.Body.Close()

	body, _ := io.ReadAll(response.Body)

	respStruct := &jsonTextToImageResponse{}

	err = json.Unmarshal(body, respStruct)
	if err != nil {
		log.Printf("API URL: %s", postURL)
		log.Printf("Unexpected API response: %s", string(body))

		return nil, err
	}

	infoStruct := &jsonInfoResponse{}

	err = json.Unmarshal([]byte(respStruct.Info), infoStruct)
	if err != nil {
		log.Printf("API URL: %s", postURL)
		log.Printf("Unexpected API response: %s", string(body))

		return nil, err
	}

	return &TextToImageResponse{
		Images:   respStruct.Images,
		Seeds:    infoStruct.AllSeeds,
		Subseeds: infoStruct.AllSubseeds,
		Model:    extractModel(respStruct.Info),
	}, nil
}

var modelRegex = regexp.MustCompile(`, (Model hash: \w+, Model: [^,]+),`)

func extractModel(infoJson string) string {
	// It's in "infotexts" string so using regex
	// <...>"infotexts": ["prompt text\\n<...>, Size: 512x512, Model hash: 1d1e459f9f, Model: anything-v4.5, <...>"], <...>
	return string(modelRegex.Find([]byte(infoJson)))
}

type UpscaleRequest struct {
	ResizeMode         int                 `json:"resize_mode"`
	UpscalingResize    int                 `json:"upscaling_resize"`
	Upscaler1          string              `json:"upscaler1"`
	TextToImageRequest *TextToImageRequest `json:"text_to_image_request"`
}

type upscaleJSONRequest struct {
	ResizeMode      int    `json:"resize_mode"`
	UpscalingResize int    `json:"upscaling_resize"`
	Upscaler1       string `json:"upscaler_1"`
	Image           string `json:"image"`
}

type UpscaleResponse struct {
	Image string `json:"image"`
}

func (api *apiImpl) UpscaleImage(upscaleReq *UpscaleRequest) (*UpscaleResponse, error) {
	if upscaleReq == nil {
		return nil, errors.New("missing request")
	}

	textToImageReq := upscaleReq.TextToImageRequest

	if textToImageReq == nil {
		return nil, errors.New("missing text to image request")
	}

	textToImageReq.NIter = 1

	regeneratedImage, err := api.TextToImage(textToImageReq)
	if err != nil {
		return nil, err
	}

	jsonReq := &upscaleJSONRequest{
		ResizeMode:      upscaleReq.ResizeMode,
		UpscalingResize: upscaleReq.UpscalingResize,
		Upscaler1:       upscaleReq.Upscaler1,
		Image:           regeneratedImage.Images[0],
	}

	postURL := api.host + "/sdapi/v1/extra-single-image"

	jsonData, err := json.Marshal(jsonReq)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequest("POST", postURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	request.Header.Set("Content-Type", "application/json; charset=UTF-8")

	client := &http.Client{}

	response, err := client.Do(request)
	if err != nil {
		log.Printf("API URL: %s", postURL)
		log.Printf("Error with API Request: %s", string(jsonData))

		return nil, err
	}

	defer response.Body.Close()

	body, _ := io.ReadAll(response.Body)

	respStruct := &UpscaleResponse{}

	err = json.Unmarshal(body, respStruct)
	if err != nil {
		log.Printf("API URL: %s", postURL)
		log.Printf("Unexpected API response: %s", string(body))

		return nil, err
	}

	return respStruct, nil
}

type ProgressResponse struct {
	Progress    float64 `json:"progress"`
	EtaRelative float64 `json:"eta_relative"`
}

func (api *apiImpl) GetCurrentProgress() (*ProgressResponse, error) {
	getURL := api.host + "/sdapi/v1/progress"

	request, err := http.NewRequest("GET", getURL, bytes.NewBuffer([]byte{}))
	if err != nil {
		return nil, err
	}

	client := &http.Client{}

	response, err := client.Do(request)
	if err != nil {
		log.Printf("API URL: %s", getURL)
		log.Printf("Error with API Request: %v", err)

		return nil, err
	}

	defer response.Body.Close()

	body, _ := io.ReadAll(response.Body)

	respStruct := &ProgressResponse{}

	err = json.Unmarshal(body, respStruct)
	if err != nil {
		log.Printf("API URL: %s", getURL)
		log.Printf("Unexpected API response: %s", string(body))

		return nil, err
	}

	return respStruct, nil
}

type Embedding struct {
	// The number of steps that were used to train this embedding, if available
	//Step int `json:"step"`
	// The hash of the checkpoint this embedding was trained on, if available
	SDCheckpoint string `json:"sd_checkpoint"`
	// The name of the checkpoint this embedding was trained on, if available. Note that this is the name that was used by the trainer; for a stable identifier, use sd_checkpoint instead
	SDCheckpointName string `json:"sd_checkpoint_name"`
	// The length of each individual vector in the embedding
	//Shape *int `json:"shape"`
	// The number of vectors in the embedding
	//Vectors *int `json:"vectors"`
}

type EmbeddingsResponseMinimal struct {
	// Embeddings loaded for the current model
	Loaded map[string]json.RawMessage
}
type EmbeddingsResponse struct {
	// Embeddings loaded for the current model
	Loaded map[string]Embedding
	// Embeddings skipped for the current model (likely due to architecture incompatibility)
	Skipped map[string]Embedding
}
type EmbeddingsResponseRaw struct {
	EmbeddingsResponseMinimal
	// Embeddings skipped for the current model (likely due to architecture incompatibility)
	Skipped map[string]json.RawMessage
}

func (api *apiImpl) GetEmbeddings() (*EmbeddingsResponseMinimal, error) {
	getURL := api.host + "/sdapi/v1/embeddings"

	request, err := http.NewRequest("GET", getURL, bytes.NewBuffer([]byte{}))
	if err != nil {
		return nil, err
	}

	client := &http.Client{}

	response, err := client.Do(request)
	if err != nil {
		log.Printf("API URL: %s", getURL)
		log.Printf("Error with API Request: %v", err)

		return nil, err
	}

	defer response.Body.Close()

	body, _ := io.ReadAll(response.Body)

	resp := &EmbeddingsResponseMinimal{}

	err = json.Unmarshal(body, &resp)
	if err != nil {
		log.Printf("API URL: %s", getURL)
		log.Printf("Unexpected API response: %s", string(body))

		return nil, err
	}

	return resp, nil
}
