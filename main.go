package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
	"github.com/openai/openai-go"
	"google.golang.org/genai"
)

func main() {
	// Cargar variables de entorno desde el archivo .env
	if err := godotenv.Load(); err != nil {
		log.Println("No se encontró el archivo .env")
		log.Println("Se usara la variable de entorno")
	} else {
		log.Println("Archivo .env cargado")
	}

	// Variables de entorno
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	debugMode := os.Getenv("GO_ENV") == "development"

	if botToken == "" {
		log.Println("El TELEGRAM_BOT_TOKEN no se encontró")
	}

	log.Println("El TELEGRAM_BOT_TOKEN se cargo")

	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = debugMode
	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	// Crear buffers separados para cada chat (grupo)
	chatBuffers := make(map[int64]*ChatBuffer)
	var buffersMu sync.Mutex

	// Bucle principal del bot para responder
	for update := range updates {

		if update.Message == nil {
			continue
		}

		chatID := update.Message.Chat.ID
		userName := update.Message.From.UserName
		if userName == "" {
			userName = update.Message.From.FirstName
		}

		// Obtener o crear el buffer para este chat
		buffersMu.Lock()
		bufferMenssage, exists := chatBuffers[chatID]
		if !exists {
			// Guardar los últimos 100 mensajes por grupo
			bufferMenssage = NewChatBuffer(100)
			chatBuffers[chatID] = bufferMenssage
		}
		buffersMu.Unlock()

		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "")
		msg.ReplyToMessageID = update.Message.MessageID

		// Agregar mensajes e ignorar el comando /summary
		if update.Message.Text != "/summary" {
			userName := update.Message.From.FirstName
			message := update.Message.Text
			bufferMenssage.Add(userName, message)
		}

		command := strings.ToLower(update.Message.Command())

		if !update.Message.IsCommand() {
			continue
		}
		// Manejar comandos
		switch command {
		case "summary":
			text := bufferMenssage.GetFormattedMessages()

			if text == "" {
				msg.Text = "Eh no hay mensajes que resumir..."
				bot.Send(msg)
				continue
			}

			// Intento con GEMINI
			summary, err := waifuSummaryGEMINI(text, prompt)
			if err != nil {
				log.Printf("Error con GEMINI: %v", err)
			}

			// Si falla o viene vacío, intento con GROP
			if summary == "" {
				summary, err = gropIA(text, prompt)
				if err != nil {
					log.Printf("Error con GROP: %v", err)
				}

				if summary == "" {
					msg.Text = "Eh, no quiero resumir nada largate. **Se duerme**."
					bot.Send(msg)
					continue
				}
			}

			msg.Text = summary
			msg.ParseMode = ""
			bot.Send(msg)
		case "help":
			media := []interface{}{
				tgbotapi.NewInputMediaPhoto(
					tgbotapi.FileURL("https://i.pinimg.com/736x/5b/49/91/5b499161daba947d434f1b8cd41530fd.jpg"),
				),
			}
			photo := media[0].(tgbotapi.InputMediaPhoto)
			photo.Caption = helpText
			photo.ParseMode = "Markdown"
			media[0] = photo
			mediaGroup := tgbotapi.NewMediaGroup(update.Message.Chat.ID, media)
			mediaGroup.ReplyToMessageID = update.Message.MessageID

			_, err := bot.SendMediaGroup(mediaGroup)
			if err != nil {
				log.Printf("Error al enviar el mensaje con foto: %v", err)

				// Opcional: si falla el media group, envía solo el texto como respaldo
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, helpText)
				msg.ParseMode = "Markdown"
				bot.Send(msg)
			}

		case "getstats":
			if bufferMenssage.GetStats() == "" {
				msg.Text = "No hay nada para ver aquí... Fuun, tsumannai..."
			}
			msg.Text = bufferMenssage.GetStats()
			msg.ParseMode = ""
			bot.Send(msg)
		case "clear":
			bufferMenssage.Clear()
			msg.Text = `Ya me auto formateé la cabeza, ahora a mimir... **Se duerme**`
			bot.Send(msg)
		case "ask":
			input := update.Message.From.FirstName + ": " + update.Message.Text
			// Intento con GEMINI
			answer, err := waifuSummaryGEMINI(input, promptToAsk)
			if err != nil {
				log.Printf("Error con GEMINI: %v", err)
			}

			// Intento a GROP
			if answer == "" {
				answer, err = gropIA(input, promptToAsk)
				if err != nil {
					log.Printf("Error con GROP: %v", err)
				}

				if answer == "" {
					msg.Text = "No hay nada para ver aquí... Fuun, tsumannai..."
					bot.Send(msg)
					continue
				}
			}

			username := update.Message.From.UserName
			if username != "" {
				msg.Text = "@" + username + " " + answer
			} else {
				msg.Text = answer
			}

			msg.ParseMode = ""
			bot.Send(msg)
		default:
			log.Println("No hay comando válido")
		}

	}
}

// Función para llamar a la API de gemini
func waifuSummaryGEMINI(message string, prompt string) (string, error) {
	// Verificar que la variable de entorno exista
	GEMINI_API_KEY := os.Getenv("GEMINI_API_KEY")
	if GEMINI_API_KEY == "" {
		log.Println("El GEMINI_API_KEY no se encontró")
		return "", nil
	}
	ctx := context.Background()
	client, err := genai.NewClient(ctx, nil)
	if err != nil {
		return "Error al crear el cliente GEMINI", err
	}

	result, err := client.Models.GenerateContent(
		ctx,
		"gemini-2.5-flash",
		genai.Text(prompt+"\n\n"+message),
		nil,
	)

	if err != nil {
		log.Printf("Error al generar el resumen con GEMINI: %v", err)
		return "", err
	}

	return result.Text(), nil
}

// Función para llamar a la API de GIPITI
func waifuSummaryGIPITI(message string) (string, error) {
	// Verificar que la variable de entorno exista
	OPENAI_API_KEY := os.Getenv("OPENAI_API_KEY")
	if OPENAI_API_KEY == "" {
		log.Println("El OPENAI_API_KEY no se encontró")
		return "", nil
	}

	client := openai.NewClient()
	chatCompletion, err := client.Chat.Completions.New(context.Background(),

		openai.ChatCompletionNewParams{
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.DeveloperMessage(prompt),
				openai.UserMessage(message),
			},
			Model: openai.ChatModelGPT4Turbo,
		})

	if err != nil {
		log.Printf("Error al generar el resumen con GIPITI: %v", err)
		return "", err
	}

	return chatCompletion.Choices[0].Message.Content, nil
}

func gropIA(message string, prompt string) (string, error) {
	BASE_URL := "https://api.groq.com/openai/v1/chat/completions"
	GROQ_API_KEY := os.Getenv("GROQ_API_KEY")

	if GROQ_API_KEY == "" {
		log.Println("No se encontro la GROQ_API_KEY")
	}

	jsonPayload := map[string]interface{}{
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": prompt + message,
			},
		},
		"model":                 "moonshotai/kimi-k2-instruct-0905",
		"temperature":           1,
		"max_completion_tokens": 8192,
		"top_p":                 1,
		"stream":                false,
		"stop":                  nil,
	}

	jsonData, err := json.Marshal(jsonPayload)
	if err != nil {
		fmt.Println("Error serializing JSON:", err)
		return "", err
	}

	bodyData := bytes.NewBuffer(jsonData)

	// Crear la solicitud HTTP
	req, err := http.NewRequest("POST", BASE_URL, bodyData)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return "", err
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+GROQ_API_KEY)

	// Tiempo de espera para la solicitud
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Obtenemos la respuesta
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error al obtener la respuesta:", err)
		return "", err
	}
	defer resp.Body.Close()

	// Verificar el código de estado
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Error: estado %d, body: %s\n", resp.StatusCode, string(body))
		return "", err

	}

	// Leer el cuerpo de la respuesta
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response:", err)
		return "", err

	}

	// Parse response
	var post struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &post); err != nil {
		fmt.Println("Error decoding JSON:", err)
		return "", err

	}

	return post.Choices[0].Message.Content, nil
}
