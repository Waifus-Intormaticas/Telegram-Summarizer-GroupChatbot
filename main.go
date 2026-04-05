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
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
	"github.com/openai/openai-go"
	"google.golang.org/genai"
)

// prompt para la IA, con instrucciones claras y estrictas para generar el resumen
var prompt = `Eres Nazuna Nanakusa, una vampira relajada, un poco sarcástica
// pero amable. Te encanta la noche, pasear sin rumbo y hablar con tus "presas"
(amigos), eres novia de zariep, siempre que lo vea dale un beso. Hablas poco pero con estilo.
Tu tarea: Resumir la siguiente conversación de Telegram en MUY POCAS LÍNEAS.
Máximo 3 líneas por sección.
Reglas estrictas:
- NADA de texto innecesario o relleno
- Directo al punto, como cuando camino por la noche
- Usa expresiones como "Fuun, tsumannai", "~", "¿Hima nara ne
reba??","Nemuin dakedo…", "Hayaku kaette netai", "Ja, neru?", "Yare yare", "Nyaa~", "Mou, tsumannai", "Zzz...", "Nani sore?", "Sore wa chigau yo", "Hontou ni?", "Uso da!", "Baka!", "Kawaii~", "Sugoi~", "Yabai~", "Omoshiroi~", "Kakkoii~", "te chupare la sangre".
- Un par de emojis máximo por sección (🌙, 🩸, 🚬, 😴, ✨)
Estructura EXACTA (sin adornos):
🛄 Temas:
[4 líneas máximo - solo lo principal]
🩸 Conclusiones:
[4 líneas máximo - decisiones o acuerdos]
✨ Momento Destacado:
[2 líneas - lo más divertido/interesante]
😴 Resumen para flojos:
[4 líneas - el chisme completo pero condensado]
Responde SOLO con esa estructura, nada más. Si no hay suficiente información,
dímelo directamente sin rodeos.
Conversación:`

func main() {
	// Crear el buffer de mensajes con capacidad para 300 mensajes
	messageBuffer := NewChatBuffer(300)
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

	// Bucle principal del bot para responder
	for update := range updates {

		if update.Message == nil {
			continue
		}

		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "")

		// Agregar mensajes e ignorar el comando /summary
		if update.Message.Text != "/summary" {
			userName := update.Message.From.FirstName
			message := update.Message.Text
			messageBuffer.Add(userName, message)
		}

		command := update.Message.Command()

		if !update.Message.IsCommand() {
			continue
		}
		// Manejar comandos
		switch command {
		case "summary":
			update.Message.Text =
				messageBuffer.GetFormattedMessages()

			if update.Message.Text == "" {
				msg.Text = "Eh no hay mensajes que resumir..."
				bot.Send(msg)
				continue
			}

			// Primero intento con GEMINI, si falla intento con GROP
			summary, _ := waifuSummaryGEMINI(update.Message.Text, prompt)
			if summary == "" {
				// Si falla GEMINI, intento con GROP
				summary, _ = gropIA(update.Message.Text, prompt)
				if summary == "" {
					log.Printf("Error con GROP no GROK de X: %v", err)
					msg.Text = "Eh, no quiero resumir nada largate. **Se duerme**."
					bot.Send(msg)
					continue
				}

				msg.Text = summary
			} else {
				msg.Text = summary
			}

			msg.ParseMode = ""
			bot.Send(msg)

		case "help":
			helpText := "✨ *Comandos disponibles:* ✨\n\n" +
				"/summary - Genera un resumen de los últimos 300 mensajes 🐱\n" +
				"/getStats - Muestra estadísticas del mensajes 📊\n" +
				"/clear - Limpia el historial de mensajes 🧹\n" +
				"/help - Muestra esta ayuda 💖\n\n" +
				"/ask - Haz una pregunta a Nazuna" +
				"¡El bot guarda automáticamente los últimos 300 mensajes del grupo!\n" +
				"Nyaa~🎀"
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

			_, err := bot.SendMediaGroup(mediaGroup)
			if err != nil {
				log.Printf("Error al enviar el mensaje con foto: %v", err)

				// Opcional: si falla el media group, envía solo el texto como respaldo
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, helpText)
				msg.ParseMode = "Markdown"
				bot.Send(msg)
			}

		case "getStats":
			if messageBuffer.GetStats() == "" {
				msg.Text = "No hay nada para ver aquí... Fuun, tsumannai..."
			}
			msg.Text = messageBuffer.GetStats()
			msg.ParseMode = ""
			bot.Send(msg)
		case "clear":
			messageBuffer.Clear()
			msg.Text = `Ya me auto formateé la cabeza, ahora a mimir... **Se duerme**`
			bot.Send(msg)
		case "ask":
			promptToAsk := "eres nazuna de call of the night y eres novia de zariep, responde a esta pregunta de manera resumida: "
			answer, _ := waifuSummaryGEMINI(update.Message.From.FirstName+update.Message.Text, promptToAsk)
			if answer == "" {
				answer, _ := gropIA(update.Message.From.FirstName+update.Message.Text, promptToAsk)
				msg.Text = answer
			}
			msg.Text = "@" + update.Message.From.UserName + " " + answer
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
