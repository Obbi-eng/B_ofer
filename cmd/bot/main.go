package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"time"

	// Telegram Imports
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"

	// WhatsApp Imports
	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// Estrutura de dados que viaja pelo Canal (Tubo) entre o Telegram e o WhatsApp
type BridgeMessage struct {
	Text string
}

func main() {
	// ==============================================
	// ⚙️ CONFIGURAÇÕES PRINCIPAIS
	// ==============================================
	apiID := 38993961                             // SUBSTITUI PELO TEU API_ID DO TELEGRAM
	apiHash := "9f58272c03543229361aab12f67d0109" // SUBSTITUI PELO TEU API_HASH DO TELEGRAM

	targetTelegramID := int64(1930892629) // O ID do grupo do Telegram que apanhaste

	// Substitui pelo JID do grupo do WhatsApp (ex: "123456789012345@g.us")
	targetWhatsAppJID := "120363409829682106@g.us"

	// 1. Cria o Canal de Comunicação (permite até 100 mensagens em fila)
	messageChannel := make(chan BridgeMessage, 100)

	// 2. Inicia o WhatsApp
	waClient := setupWhatsApp()
	defer waClient.Disconnect()

	// 3. Inicia a rotina de Envio do WhatsApp em segundo plano (Goroutine)
	go whatsappSender(waClient, targetWhatsAppJID, messageChannel)

	// 4. Inicia o Telegram e bloqueia o programa para ficar a ouvir eternamente
	startTelegramListener(apiID, apiHash, targetTelegramID, messageChannel)
}

// ==========================================
// ➡️ COMPONENTE: DISPARADOR DO WHATSAPP
// ==========================================
func whatsappSender(client *whatsmeow.Client, targetJID string, msgChan <-chan BridgeMessage) {
	groupJID, err := types.ParseJID(targetJID)
	if err != nil {
		log.Printf("⚠️ Erro no JID do WhatsApp: %v", err)
		return
	}

	for msg := range msgChan {
		// 1. Formata o texto final
		finalText := fmt.Sprintf("🤖 *Encaminhado do Telegram*\n\n%s", msg.Text)

		// 2. Atraso Humano Aleatório (evita banimentos por spamming)
		delay := time.Duration(rand.Intn(4)+2) * time.Second
		time.Sleep(delay)

		// 3. Envia a mensagem
		waMsg := &waProto.Message{
			Conversation: proto.String(finalText),
		}

		_, err := client.SendMessage(context.Background(), groupJID, waMsg)
		if err != nil {
			log.Printf("❌ [WhatsApp] Falha ao enviar: %v", err)
		} else {
			log.Printf("✅ [WhatsApp] Mensagem encaminhada com sucesso para o grupo!")
		}
	}
}

// ==========================================
// ⬅️ COMPONENTE: OUVINTE DO TELEGRAM
// ==========================================
func startTelegramListener(apiID int, apiHash string, targetID int64, msgChan chan<- BridgeMessage) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	dispatcher := tg.NewUpdateDispatcher()

	// Ouve apenas as mensagens de Supergrupos/Canais
	dispatcher.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateNewChannelMessage) error {
		msg, ok := update.Message.(*tg.Message)
		if ok && msg.Message != "" {
			peer, isChannel := msg.PeerID.(*tg.PeerChannel)

			// Se o ID da mensagem bater certo com o ID que configurámos
			if isChannel && peer.ChannelID == targetID {
				fmt.Printf("🔍 [Telegram] Nova mensagem detetada: %s\n", msg.Message)

				// Atira a mensagem para dentro do tubo (Canal Go)
				msgChan <- BridgeMessage{Text: msg.Message}
			}
		}
		return nil
	})

	client := telegram.NewClient(apiID, apiHash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: "tg_session.json"},
		UpdateHandler:  dispatcher,
	})

	fmt.Println("🚀 Iniciando Ponte Telegram -> WhatsApp...")

	err := client.Run(ctx, func(ctx context.Context) error {
		flow := auth.NewFlow(TerminalAuth{}, auth.SendCodeOptions{})
		if err := client.Auth().IfNecessary(ctx, flow); err != nil {
			return err
		}

		user, _ := client.Self(ctx)
		fmt.Printf("✅ Telegram conectado (UserBot): %s\n", user.Username)
		fmt.Println("🎧 A aguardar mensagens... Pressiona Ctrl+C para desligar.")

		<-ctx.Done()
		fmt.Println("\nA desligar de forma segura...")
		return nil
	})

	if err != nil {
		panic(err)
	}
}

// ==========================================
// ⚙️ INICIALIZAÇÃO DO WHATSAPP (SESSÃO)
// ==========================================
func setupWhatsApp() *whatsmeow.Client {
	dbLog := waLog.Stdout("Database", "ERROR", false) // Silenciei os logs do DB para o ecrã ficar mais limpo
	container, err := sqlstore.New(context.Background(), "sqlite3", "file:wa_session.db?_foreign_keys=on", dbLog)
	if err != nil {
		panic(err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		panic(err)
	}

	clientLog := waLog.Stdout("Client", "ERROR", false)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	err = client.Connect()
	if err != nil {
		panic(err)
	}

	fmt.Println("✅ WhatsApp conectado (usando a sessão guardada)!")
	return client
}

// --- Funções Auxiliares de Login (Obrigatórias para a Interface compilar) ---
type TerminalAuth struct{}

func (TerminalAuth) Phone(_ context.Context) (string, error)    { return "", nil }
func (TerminalAuth) Password(_ context.Context) (string, error) { return "", nil }
func (TerminalAuth) AcceptTermsOfService(_ context.Context, tos tg.HelpTermsOfService) error {
	return nil
}
func (TerminalAuth) SignUp(_ context.Context) (auth.UserInfo, error)            { return auth.UserInfo{}, nil }
func (TerminalAuth) Code(_ context.Context, _ *tg.AuthSentCode) (string, error) { return "", nil }
