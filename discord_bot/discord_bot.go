package discord_bot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"stable_diffusion_bot/imagine_queue"
	"stable_diffusion_bot/repositories/statistics"
	"stable_diffusion_bot/stable_diffusion_api"

	"github.com/bwmarrin/discordgo"
)

type botImpl struct {
	developmentMode    bool
	botSession         *discordgo.Session
	guildID            string
	imagineQueue       imagine_queue.Queue
	registeredCommands []*discordgo.ApplicationCommand
	imagineCommand     string
	removeCommands     bool
	stableDiffusionAPI stable_diffusion_api.StableDiffusionAPI
	statisticsRepo     statistics.Repository
}

type Config struct {
	DevelopmentMode    bool
	BotToken           string
	GuildID            string
	ImagineQueue       imagine_queue.Queue
	ImagineCommand     string
	RemoveCommands     bool
	StableDiffusionAPI stable_diffusion_api.StableDiffusionAPI
	StatisticsRepo     statistics.Repository
}

func (b *botImpl) imagineCommandString() string {
	if b.developmentMode {
		return "dev_" + b.imagineCommand
	}

	return b.imagineCommand
}

func (b *botImpl) imagineExtCommandString() string {
	prefix := ``
	if b.developmentMode {
		prefix = `dev_`
	}

	return prefix + b.imagineCommand + `_ext`
}

func (b *botImpl) imagineSettingsCommandString() string {
	if b.developmentMode {
		return "dev_" + b.imagineCommand + "_settings"
	}

	return b.imagineCommand + "_settings"
}

func (b *botImpl) imagineStatsCommandString() string {
	if b.developmentMode {
		return "dev_" + b.imagineCommand + "_stats"
	}

	return b.imagineCommand + "_stats"
}

func New(cfg Config) (Bot, error) {
	if cfg.BotToken == "" {
		return nil, errors.New("missing bot token")
	}

	if cfg.GuildID == "" {
		return nil, errors.New("missing guild ID")
	}

	if cfg.ImagineQueue == nil {
		return nil, errors.New("missing imagine queue")
	}

	if cfg.ImagineCommand == "" {
		return nil, errors.New("missing imagine command")
	}

	if cfg.StableDiffusionAPI == nil {
		return nil, errors.New("missing stable diffusion API")
	}

	if cfg.StatisticsRepo == nil {
		return nil, errors.New("missing statistics repo")
	}

	botSession, err := discordgo.New("Bot " + cfg.BotToken)
	if err != nil {
		return nil, err
	}

	botSession.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Logged in as: %v#%v", s.State.User.Username, s.State.User.Discriminator)
	})
	err = botSession.Open()
	if err != nil {
		return nil, err
	}

	bot := &botImpl{
		developmentMode:    cfg.DevelopmentMode,
		botSession:         botSession,
		imagineQueue:       cfg.ImagineQueue,
		registeredCommands: make([]*discordgo.ApplicationCommand, 0),
		imagineCommand:     cfg.ImagineCommand,
		removeCommands:     cfg.RemoveCommands,
		stableDiffusionAPI: cfg.StableDiffusionAPI,
		statisticsRepo:     cfg.StatisticsRepo,
	}

	err = bot.addImagineCommand()
	if err != nil {
		return nil, err
	}

	err = bot.addImagineExtCommand()
	if err != nil {
		return nil, err
	}

	err = bot.addImagineSettingsCommand()
	if err != nil {
		return nil, err
	}

	err = bot.addStatsCommand()
	if err != nil {
		return nil, err
	}

	botSession.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		switch i.Type {
		case discordgo.InteractionApplicationCommand:
			switch i.ApplicationCommandData().Name {
			case bot.imagineCommandString():
				bot.processImagineCommand(s, i)
			case bot.imagineExtCommandString():
				bot.processImagineExtCommand(s, i)
			case bot.imagineSettingsCommandString():
				bot.processImagineSettingsCommand(s, i)
			case bot.imagineStatsCommandString():
				bot.processImagineStatsCommand(s, i)
			default:
				log.Printf("Unknown command '%v'", i.ApplicationCommandData().Name)
			}
		case discordgo.InteractionMessageComponent:
			switch customID := i.MessageComponentData().CustomID; {
			case customID == "imagine_reroll":
				bot.processImagineReroll(s, i)
			case strings.HasPrefix(customID, "imagine_upscale_"):
				interactionIndex := strings.TrimPrefix(customID, "imagine_upscale_")

				interactionIndexInt, intErr := strconv.Atoi(interactionIndex)
				if intErr != nil {
					log.Printf("Error parsing interaction index: %v", err)

					return
				}

				bot.processImagineUpscale(s, i, interactionIndexInt)
			case strings.HasPrefix(customID, "imagine_variation_"):
				interactionIndex := strings.TrimPrefix(customID, "imagine_variation_")

				interactionIndexInt, intErr := strconv.Atoi(interactionIndex)
				if intErr != nil {
					log.Printf("Error parsing interaction index: %v", err)

					return
				}

				bot.processImagineVariation(s, i, interactionIndexInt)
			case customID == "imagine_dimension_setting_menu":
				if len(i.MessageComponentData().Values) == 0 {
					log.Printf("No values for imagine dimension setting menu")

					return
				}

				sizes := strings.Split(i.MessageComponentData().Values[0], "_")

				width := sizes[0]
				height := sizes[1]

				widthInt, intErr := strconv.Atoi(width)
				if intErr != nil {
					log.Printf("Error parsing width: %v", err)

					return
				}

				heightInt, intErr := strconv.Atoi(height)
				if intErr != nil {
					log.Printf("Error parsing height: %v", err)

					return
				}

				bot.processImagineDimensionSetting(s, i, widthInt, heightInt)
			default:
				log.Printf("Unknown message component '%v'", i.MessageComponentData().CustomID)
			}
		}
	})

	return bot, nil
}

func (b *botImpl) Start() {
	b.imagineQueue.StartPolling(b.botSession)

	err := b.teardown()
	if err != nil {
		log.Printf("Error tearing down bot: %v", err)
	}
}

func (b *botImpl) teardown() error {
	// Delete all commands added by the bot
	if b.removeCommands {
		log.Printf("Removing all commands added by bot...")

		for _, v := range b.registeredCommands {
			log.Printf("Removing command '%v'...", v.Name)

			err := b.botSession.ApplicationCommandDelete(b.botSession.State.User.ID, b.guildID, v.ID)
			if err != nil {
				log.Panicf("Cannot delete '%v' command: %v", v.Name, err)
			}
		}
	}

	return b.botSession.Close()
}

func (b *botImpl) addImagineCommand() error {
	log.Printf("Adding command '%s'...", b.imagineCommandString())

	cmd, err := b.botSession.ApplicationCommandCreate(b.botSession.State.User.ID, b.guildID, &discordgo.ApplicationCommand{
		Name:        b.imagineCommandString(),
		Description: "Ask the bot to imagine something",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "prompt",
				Description: "The text prompt to imagine",
				Required:    true,
			},
		},
	})
	if err != nil {
		log.Printf("Error creating '%s' command: %v", b.imagineCommandString(), err)

		return err
	}

	b.registeredCommands = append(b.registeredCommands, cmd)

	return nil
}

const (
	extOptionAR             = `aspect_ratio`
	extOptionCFGScale       = `cfg_scale`
	extOptionEmbeddings     = `embeddings`
	extOptionNegativePrompt = `negative_prompt`
	extOptionPrompt         = `prompt`
	extOptionRestoreFaces   = `restore_faces`
	extOptionSampler        = `sampler`
	extOptionSeed           = `seed`
	extOptionSteps          = `steps`
)

func (b *botImpl) addImagineExtCommand() error {
	command := b.imagineExtCommandString()
	log.Printf("Adding command '%s'...", command)

	minNum := 1.0
	commandOptions := []*discordgo.ApplicationCommandOption{
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        extOptionPrompt,
			Description: "The text prompt to imagine (`--ar x:y` to set aspect ratio)",
			Required:    true,
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        extOptionAR,
			Description: "Aspect Ratio",
			Required:    false,
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				{
					Name:  "1:1  (square, 512×512)",
					Value: "",
				},
				{
					Name:  "4:3  (horizontal, 688×512)",
					Value: "--ar 4:3",
				},
				{
					Name:  "16:10 (horizontal wide, 824×512)",
					Value: "--ar 16:10",
				},
				{
					Name:  "16:9 (horizontal wide, 912×512)",
					Value: "--ar 16:9",
				},
				{
					Name:  "3:4 (vertical, 512×688)",
					Value: "--ar 3:4",
				},
				{
					Name:  "10:16 (vertical narrow, 512×824)",
					Value: "--ar 10:16",
				},
				{
					Name:  "9:16 (vertical narrow, 512×912)",
					Value: "--ar 9:16",
				},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        extOptionNegativePrompt,
			Description: "Negative prompt",
			Required:    false,
		},
		{
			Type:        discordgo.ApplicationCommandOptionBoolean,
			Name:        extOptionRestoreFaces,
			Description: "Restore faces" + fmt.Sprintf(" (%v)", imagine_queue.DefaultRestoreFaces),
			Required:    false,
		},
		{
			Type:        discordgo.ApplicationCommandOptionNumber,
			Name:        extOptionCFGScale,
			Description: fmt.Sprintf("CFG Scale (%d)", imagine_queue.DefaultCFGScale),
			Required:    false,
			MinValue:    &minNum,
			MaxValue:    30,
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        extOptionSeed,
			Description: fmt.Sprintf("Seed (%d)", imagine_queue.DefaultSeed),
			Required:    false,
		},
		{
			Type:        discordgo.ApplicationCommandOptionString,
			Name:        extOptionSampler,
			Description: fmt.Sprintf("Sampler (%s)", imagine_queue.DefaultSampler),
			Required:    false,
			Choices: []*discordgo.ApplicationCommandOptionChoice{
				// TODO: move to config
				{
					Name:  "DPM++ 2M Karras",
					Value: "DPM++ 2M Karras",
				},
				{
					Name:  "Euler a",
					Value: "Euler a",
				},
				{
					Name:  "DDIM",
					Value: "DDIM",
				},
				{
					Name:  "PLMS",
					Value: "PLMS",
				},
				{
					Name:  "UniPC",
					Value: "UniPC",
				},
				{
					Name:  "Heun",
					Value: "Heun",
				},
				{
					Name:  "Euler",
					Value: "Euler",
				},
				{
					Name:  "LMS",
					Value: "LMS",
				},
				{
					Name:  "LMS Karras",
					Value: "LMS Karras",
				},
				{
					Name:  "DPM2 a",
					Value: "DPM2 a",
				},
				{
					Name:  "DPM2 a Karras",
					Value: "DPM2 a Karras",
				},
				{
					Name:  "DPM2",
					Value: "DPM2",
				},
				{
					Name:  "DPM2 Karras",
					Value: "DPM2 Karras",
				},
				{
					Name:  "DPM fast",
					Value: "DPM fast",
				},
				{
					Name:  "DPM adaptive",
					Value: "DPM adaptive",
				},
				{
					Name:  "DPM++ 2S a",
					Value: "DPM++ 2S a",
				},
				{
					Name:  "DPM++ 2M",
					Value: "DPM++ 2M",
				},
				{
					Name:  "DPM++ SDE",
					Value: "DPM++ SDE",
				},
				{
					Name:  "DPM++ 2S a Karras",
					Value: "DPM++ 2S a Karras",
				},
				{
					Name:  "DPM++ SDE Karras",
					Value: "DPM++ SDE Karras",
				},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        extOptionSteps,
			Description: fmt.Sprintf("Sampling Steps (%d)", imagine_queue.DefaultSteps),
			MinValue:    &minNum,
			MaxValue:    50,
		},
	}

	// TODO: reload embeddings on model change
	embs, err := b.stableDiffusionAPI.GetEmbeddings()
	if err != nil {
		log.Printf("Error getting embeddings: %v", err)
	}
	if len(embs.Loaded) > 0 {
		var options []*discordgo.ApplicationCommandOptionChoice
		for embed := range embs.Loaded {
			options = append(options, &discordgo.ApplicationCommandOptionChoice{
				Name:  embed,
				Value: embed,
			})

			// Max 25 choices
			// https://discord.com/developers/docs/interactions/application-commands#application-command-object-application-command-option-structure
			if len(options) == 25 {
				log.Printf("Loaded 25/%d textual inversions...", len(embs.Loaded))
				break
			}
		}

		commandOptions = append(commandOptions, &discordgo.ApplicationCommandOption{
			Type:         discordgo.ApplicationCommandOptionString,
			Name:         extOptionEmbeddings,
			Description:  "Textual Inversion",
			Required:     false,
			Autocomplete: false,
			Choices:      options,
		})
	}

	cmd, err := b.botSession.ApplicationCommandCreate(b.botSession.State.User.ID, b.guildID, &discordgo.ApplicationCommand{
		Name:        command,
		Description: "Ask the bot to imagine something",
		Options:     commandOptions,
	})
	if err != nil {
		log.Printf("Error creating '%s' command: %v", command, err)

		return err
	}

	b.registeredCommands = append(b.registeredCommands, cmd)

	return nil
}

func (b *botImpl) addImagineSettingsCommand() error {
	log.Printf("Adding command '%s'...", b.imagineSettingsCommandString())

	cmd, err := b.botSession.ApplicationCommandCreate(b.botSession.State.User.ID, b.guildID, &discordgo.ApplicationCommand{
		Name:        b.imagineSettingsCommandString(),
		Description: "Change the default settings for the imagine command",
	})
	if err != nil {
		log.Printf("Error creating '%s' command: %v", b.imagineSettingsCommandString(), err)

		return err
	}

	b.registeredCommands = append(b.registeredCommands, cmd)

	return nil
}

const statsOptionUser = `user`

func (b *botImpl) addStatsCommand() error {
	log.Printf("Adding command '%s'...", b.imagineStatsCommandString())

	cmd, err := b.botSession.ApplicationCommandCreate(b.botSession.State.User.ID, b.guildID, &discordgo.ApplicationCommand{
		Name:        b.imagineStatsCommandString(),
		Description: "Show generation statistics",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionUser,
				Name:        statsOptionUser,
				Description: "Show stats for user",
			},
		},
	})
	if err != nil {
		log.Printf("Error creating '%s' command: %v", b.imagineStatsCommandString(), err)

		return err
	}

	b.registeredCommands = append(b.registeredCommands, cmd)

	return nil
}

func (b *botImpl) processImagineReroll(s *discordgo.Session, i *discordgo.InteractionCreate) {
	position, queueError := b.imagineQueue.AddImagine(&imagine_queue.QueueItem{
		Type:               imagine_queue.ItemTypeReroll,
		DiscordInteraction: i.Interaction,
	})
	if queueError != nil {
		log.Printf("Error adding imagine to queue: %v\n", queueError)
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("I'm reimagining that for you... You are currently #%d in line.", position),
		},
	})
	if err != nil {
		log.Printf("Error responding to interaction: %v", err)
	}
}

func (b *botImpl) processImagineUpscale(s *discordgo.Session, i *discordgo.InteractionCreate, upscaleIndex int) {
	position, queueError := b.imagineQueue.AddImagine(&imagine_queue.QueueItem{
		Type:               imagine_queue.ItemTypeUpscale,
		InteractionIndex:   upscaleIndex,
		DiscordInteraction: i.Interaction,
	})
	if queueError != nil {
		log.Printf("Error adding imagine to queue: %v\n", queueError)
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("I'm upscaling that for you... You are currently #%d in line.", position),
		},
	})
	if err != nil {
		log.Printf("Error responding to interaction: %v", err)
	}
}

func (b *botImpl) processImagineVariation(s *discordgo.Session, i *discordgo.InteractionCreate, variationIndex int) {
	position, queueError := b.imagineQueue.AddImagine(&imagine_queue.QueueItem{
		Type:               imagine_queue.ItemTypeVariation,
		InteractionIndex:   variationIndex,
		DiscordInteraction: i.Interaction,
	})
	if queueError != nil {
		log.Printf("Error adding imagine to queue: %v\n", queueError)
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("I'm imagining more variations for you... You are currently #%d in line.", position),
		},
	})
	if err != nil {
		log.Printf("Error responding to interaction: %v", err)
	}
}

// TODO: add option to enable usage in DM
func (b *botImpl) processImagineCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options

	optionMap := make(map[string]*discordgo.ApplicationCommandInteractionDataOption, len(options))
	for _, opt := range options {
		optionMap[opt.Name] = opt
	}

	var position int
	var queueError error
	var prompt string

	// Do not allow DM usage
	isDM := i.GuildID == ""

	if option, ok := optionMap["prompt"]; ok {
		prompt = option.StringValue()

		if !isDM {
			position, queueError = b.imagineQueue.AddImagine(&imagine_queue.QueueItem{
				Prompt:             prompt,
				Options:            imagine_queue.NewQueueItemOptions(),
				Type:               imagine_queue.ItemTypeImagine,
				DiscordInteraction: i.Interaction,
			})
			if queueError != nil {
				log.Printf("Error adding imagine to queue: %v\n", queueError)
			}
		}
	}

	message := "DM usage is not allowed."
	if !isDM {
		message = fmt.Sprintf(
			"I'm dreaming something up for you. You are currently #%d in line.\n<@%s> asked me to imagine \"%s\".",
			position,
			i.Member.User.ID,
			prompt,
		)
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
		},
	})
	if err != nil {
		log.Printf("Error send interaction resp: %v\n", err)
	}
}

func (b *botImpl) processImagineExtCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options

	queueOptions := imagine_queue.NewQueueItemOptions()
	aspectRatio := ""
	for _, opt := range options {
		switch opt.Name {
		case extOptionAR:
			aspectRatio = opt.StringValue()

			if aspectRatio != "" {
				queueOptions.Prompt += ` ` + aspectRatio
			}
		case extOptionPrompt:
			queueOptions.Prompt = opt.StringValue()
		case extOptionNegativePrompt:
			queueOptions.NegativePrompt = opt.StringValue()
		case extOptionRestoreFaces:
			queueOptions.RestoreFaces = opt.BoolValue()
		case extOptionCFGScale:
			queueOptions.CfgScale = opt.FloatValue()
		case extOptionSeed:
			queueOptions.Seed = int(opt.IntValue())
		case extOptionSampler:
			queueOptions.SamplerName = opt.StringValue()
		case extOptionEmbeddings:
			queueOptions.Prompt += `, ` + opt.StringValue()
		case extOptionSteps:
			queueOptions.Steps = int(opt.IntValue())
		}
	}

	var position int
	var queueError error

	// Do not allow DM usage
	isDM := i.GuildID == ""

	if !isDM {
		position, queueError = b.imagineQueue.AddImagine(&imagine_queue.QueueItem{
			Prompt:             queueOptions.Prompt,
			Options:            queueOptions,
			Type:               imagine_queue.ItemTypeImagine,
			DiscordInteraction: i.Interaction,
		})
		if queueError != nil {
			log.Printf("Error adding imagine to queue: %v\n", queueError)
		}
	}

	message := "DM usage is not allowed."
	if !isDM {
		message = fmt.Sprintf(
			"I'm dreaming something up for you. You are currently #%d in line.\n<@%s> asked me to imagine `%s`.",
			position,
			i.Member.User.ID,
			queueOptions.Prompt,
		)
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
		},
	})
	if err != nil {
		log.Printf("Error send interaction resp: %v\n", err)
	}
}

func (b *botImpl) processImagineSettingsCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	defaultWidth, err := b.imagineQueue.GetDefaultBotWidth()
	if err != nil {
		log.Printf("error getting default width for settings command: %v", err)
	}

	defaultHeight, err := b.imagineQueue.GetDefaultBotHeight()
	if err != nil {
		log.Printf("error getting default height for settings command: %v", err)
	}

	minValues := 1

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Title:   "Settings",
			Content: "Choose defaults settings for the imagine command:",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.SelectMenu{
							CustomID:  "imagine_dimension_setting_menu",
							MinValues: &minValues,
							MaxValues: 1,
							Options: []discordgo.SelectMenuOption{
								{
									Label:   "Size: 512x512",
									Value:   "512_512",
									Default: defaultWidth == 512 && defaultHeight == 512,
								},
								{
									Label:   "Size: 768x768",
									Value:   "768_768",
									Default: defaultWidth == 768 && defaultHeight == 768,
								},
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		log.Printf("Error responding to interaction: %v", err)
	}
}

func (b *botImpl) processImagineStatsCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	message := "Something wrong."

	options := i.ApplicationCommandData().Options

	member := i.Member.User
	for _, opt := range options {
		switch opt.Name {
		case statsOptionUser:
			member = opt.UserValue(s)
		}
	}

	stats, err := b.statisticsRepo.GetStatByMember(context.Background(), member.ID)
	if err != nil {
		log.Print("Error getting stats: ", err)
	} else if stats == nil {
		message = "No statistics found."
	} else {
		message = fmt.Sprintf("<@%s> generated %d images. Total time: %s", stats.MemberID, stats.Count, (time.Duration(stats.TimeMs) * time.Millisecond).Round(time.Second).String())
	}

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
		},
	})
	if err != nil {
		log.Printf("Error responding to interaction: %v", err)
	}
}

func (b *botImpl) processImagineDimensionSetting(s *discordgo.Session, i *discordgo.InteractionCreate, height, width int) {
	err := b.imagineQueue.UpdateDefaultDimensions(width, height)
	if err != nil {
		log.Printf("error updating default dimensions: %v", err)

		err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content: "Error updating default dimensions...",
			},
		})
		if err != nil {
			log.Printf("Error responding to interaction: %v", err)
		}

		return
	}

	minValues := 1

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content: "Choose defaults settings for the imagine command:",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.SelectMenu{
							CustomID:  "imagine_dimension_setting_menu",
							MinValues: &minValues,
							MaxValues: 1,
							Options: []discordgo.SelectMenuOption{
								{
									Label:   "Size: 512x512",
									Value:   "512_512",
									Default: width == 512 && height == 512,
								},
								{
									Label:   "Size: 768x768",
									Value:   "768_768",
									Default: width == 768 && height == 768,
								},
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		log.Printf("Error responding to interaction: %v", err)
	}
}
