package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/mdp/qrterminal/v3" // Opcional: para mostrar o QR Code no terminal
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"

	_ "github.com/mattn/go-sqlite3"
)

// Estrutura de dados que trafega entre o Telegram e o WhatsApp
type RawMessage struct {
	Text   string
	Author string
}

func main() {
	// Variáveis de ambiente (você pode usar .env no futuro)
	tgToken := "SEU_TOKEN_DO_TELEGRAM_AQUI"
	waTargetGroupJID := "1234567890-123456@g.us" // ID do seu grupo no WhatsApp

	// Canal de comunicação entre os sistemas (desacoplamento)
	messageChannel := make(chan RawMessage, 100) // Buffer de 100 mensagens

	// 1. Inicializa o WhatsApp (Consumidor)
	waClient := setupWhatsApp()
	defer waClient.Disconnect()

	// Inicia a Goroutine do WhatsApp Dispatcher
	go whatsappDispatcher(waClient, waTargetGroupJID, messageChannel)

	// 2. Inicializa o Telegram (Produtor)
	telegramListener(tgToken, messageChannel)
}

// ==========================================
// COMPONENTE 1: Telegram Listener
// ==========================================
func telegramListener(token string, msgChan chan<- RawMessage) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Panic("Erro ao conectar no Telegram: ", err)
	}

	bot.Debug = false
	log.Printf("Autorizado na conta Telegram: %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	// Escuta eventos em loop
	for update := range updates {
		if update.Message != nil {
			// Filtra apenas mensagens de texto (você pode expandir para imagens depois)
			if update.Message.Text != "" {
				log.Printf("[Telegram] Nova mensagem de %s", update.Message.From.UserName)

				// Envia para o canal
				msgChan <- RawMessage{
					Text:   update.Message.Text,
					Author: update.Message.From.UserName,
				}
			}
		}
	}
}

// ==========================================
// COMPONENTE 2: Message Parser / Formatter
// ==========================================
func formatMessage(raw RawMessage) string {
	// Aqui entram suas regras de negócio (Regex, substituições, etc)
	formattedText := strings.TrimSpace(raw.Text)

	// Exemplo de template
	return fmt.Sprintf("🤖 *Encaminhado do Telegram*\n👤 *Autor:* %s\n\n%s", raw.Author, formattedText)
}

// ==========================================
// COMPONENTE 3: WhatsApp Dispatcher
// ==========================================
func whatsappDispatcher(waClient *whatsmeow.Client, targetJID string, msgChan <-chan RawMessage) {
	// Converte a string do JID para o tipo JID do whatsmeow
	groupJID, err := types.ParseJID(targetJID)
	if err != nil {
		log.Fatalf("JID do WhatsApp inválido: %v", err)
	}

	// Consome o canal eternamente
	for rawMsg := range msgChan {
		// 1. Formata a mensagem
		finalText := formatMessage(rawMsg)

		// 2. Delay Humano (evita banimentos por spam em rajada)
		delay := time.Duration(rand.Intn(3)+2) * time.Second
		time.Sleep(delay)

		// 3. Monta o payload e envia
		msg := &waProto.Message{
			Conversation: &finalText,
		}

		_, err := waClient.SendMessage(context.Background(), groupJID, msg)
		if err != nil {
			log.Printf("[WhatsApp] Erro ao enviar mensagem: %v", err)
		} else {
			log.Printf("[WhatsApp] Mensagem enviada com sucesso para %s", targetJID)
		}
	}
}

// ==========================================
// COMPONENTE 4: Session & State Manager
// ==========================================
func setupWhatsApp() *whatsmeow.Client {
	dbLog := waLog.Stdout("Database", "WARN", true)
	// Usa o SQLite embutido para salvar a sessão e não precisar ler QR Code toda vez
	container, err := sqlstore.New("sqlite3", "file:wa_session.db?_foreign_keys=on", dbLog)
	if err != nil {
		panic(err)
	}

	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		panic(err)
	}

	clientLog := waLog.Stdout("Client", "WARN", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	if client.Store.ID == nil {
		// Nenhuma sessão salva, precisa escanear o QR Code
		qrChan, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
			panic(err)
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				// Mostra o QR Code no terminal. Requer a lib qrterminal
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				fmt.Println("Escaneie o QR Code acima com seu WhatsApp")
			} else {
				fmt.Println("Login event:", evt.Event)
			}
		}
	} else {
		// Já tem sessão, apenas conecta
		err = client.Connect()
		if err != nil {
			panic(err)
		}
	}

	return client
}
