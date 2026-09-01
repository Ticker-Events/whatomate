import {
  collection,
  onSnapshot,
  query,
  where,
  orderBy,
  type Unsubscribe
} from 'firebase/firestore'
import { signInWithCustomToken } from 'firebase/auth'
import { getFirebaseAuth, getFirebaseDB, isFirebaseConfigured } from '@/lib/firebase'
import { useContactsStore, type Contact, type Message } from '@/stores/contacts'
import { useAuthStore } from '@/stores/auth'
import { authService, contactsService } from '@/services/api'
import { getActiveOrganizationId } from '@/lib/organization'
import { toast } from 'vue-sonner'
import router from '@/router'

// Notification sound
let notificationSound: HTMLAudioElement | null = null

function playNotificationSound() {
  if (!notificationSound) {
    notificationSound = new Audio('/notification.mp3')
    notificationSound.volume = 0.5
  }
  notificationSound.currentTime = 0
  notificationSound.play().catch(() => {
    // Ignore autoplay errors
  })
}

function showNotification(title: string, body: string, contactId: string) {
  toast.info(title, {
    description: body,
    duration: 5000,
    action: {
      label: 'View',
      onClick: () => {
        router.push(`/chat/${contactId}`)
      },
      actionButtonStyle: {
        background: 'transparent',
        border: '1px solid #e5e7eb',
        color: '#3b82f6',
        fontWeight: '500'
      }
    }
  })
}

function mapFirestoreMessage(docId: string, data: Record<string, any>): Message {
  return {
    id: docId,
    contact_id: data.contactId,
    direction: data.direction,
    message_type: data.messageType,
    content: data.content,
    media_url: data.mediaUrl,
    media_mime_type: data.mediaMimeType,
    media_filename: data.mediaFilename,
    interactive_data: data.interactiveData,
    status: data.status,
    wamid: data.wamid,
    error_message: data.errorMessage,
    is_reply: data.isReply,
    reply_to_message_id: data.replyToMessageId,
    reply_to_message: data.replyToMessage,
    whatsapp_account: data.whatsappAccount,
    created_at: data.createdAt,
    updated_at: data.updatedAt
  }
}

function mapFirestoreContact(docId: string, data: Record<string, any>): Partial<Contact> {
  const lastMessage = data.lastMessageInfo
  return {
    id: docId,
    phone_number: data.phoneNumber,
    profile_name: data.profileName,
    name: data.profileName,
    assigned_user_id: data.assignedUserId,
    whatsapp_account: data.whatsappAccount,
    last_message_at: data.lastMessageAt,
    last_inbound_at: data.lastInboundAt,
    unread_count: data.unreadCount ?? 0,
    status: 'active',
    tags: [],
    metadata: {},
    created_at: data.createdAt || new Date().toISOString(),
    updated_at: data.updatedAt || new Date().toISOString(),
    service_window_open: data.lastInboundAt
      ? Date.now() - new Date(data.lastInboundAt).getTime() < 24 * 60 * 60 * 1000
      : false,
    last_message_preview: lastMessage?.content?.body?.substring(0, 100) || ''
  }
}

class FirestoreService {
  private contactsUnsubscribe: Unsubscribe | null = null
  private messagesUnsubscribe: Unsubscribe | null = null
  private isConnected = false
  private viewingContactId: string | null = null

  async connect() {
    if (!isFirebaseConfigured() || this.isConnected) {
      return
    }

    try {
      const resp = await authService.getFirebaseToken()
      const token = resp.data.data?.token || resp.data.token
      if (!token) {
        return
      }

      await signInWithCustomToken(getFirebaseAuth(), token)
      this.isConnected = true
      this.subscribeToContacts()
    } catch {
      // Firebase optional — chat still works via REST
    }
  }

  disconnect() {
    this.unsubscribeContacts()
    this.unsubscribeMessages()
    this.isConnected = false
    this.viewingContactId = null
  }

  setViewingContact(contactId: string | null) {
    this.viewingContactId = contactId
    if (contactId) {
      this.subscribeToMessages(contactId)
    } else {
      this.unsubscribeMessages()
    }
  }

  private subscribeToContacts() {
    const authStore = useAuthStore()
    const orgId = getActiveOrganizationId(authStore.organizationId)
    if (!orgId) return

    this.unsubscribeContacts()

    const q = query(
      collection(getFirebaseDB(), 'contacts'),
      where('organizationId', '==', orgId)
    )

    this.contactsUnsubscribe = onSnapshot(q, (snapshot) => {
      const store = useContactsStore()
      snapshot.docChanges().forEach((change) => {
        const data = change.doc.data() as Record<string, any>
        if (data.organizationId && data.organizationId !== orgId) {
          return
        }
        const partial = mapFirestoreContact(change.doc.id, data)
        if (change.type === 'removed') {
          return
        }
        store.applyContactFirestoreUpdate(partial as Contact)
      })
    })
  }

  private subscribeToMessages(contactId: string) {
    const authStore = useAuthStore()
    const orgId = getActiveOrganizationId(authStore.organizationId)
    if (!orgId) return

    this.unsubscribeMessages()

    const q = query(
      collection(getFirebaseDB(), 'messages'),
      where('organizationId', '==', orgId),
      where('contactId', '==', contactId),
      orderBy('createdAt')
    )

    this.messagesUnsubscribe = onSnapshot(q, (snapshot) => {
      const store = useContactsStore()
      const authStore = useAuthStore()

      snapshot.docChanges().forEach((change) => {
        const data = change.doc.data() as Record<string, any>
        if (data.organizationId && data.organizationId !== orgId) {
          return
        }
        const message = mapFirestoreMessage(change.doc.id, data)

        if (change.type === 'added') {
          this.handleNewMessage(store, authStore, message, contactId, data)
        } else if (change.type === 'modified') {
          store.updateMessageStatus(message.id, message.status, message.error_message)
        }
      })
    })
  }

  private handleNewMessage(
    store: ReturnType<typeof useContactsStore>,
    authStore: ReturnType<typeof useAuthStore>,
    message: Message,
    contactId: string,
    data: Record<string, any>
  ) {
    const currentContact = store.currentContact
    const isViewingThisContact = currentContact && contactId === currentContact.id

    if (isViewingThisContact) {
      store.addMessage(message)
    }

    if (message.direction === 'incoming' && !isViewingThisContact) {
      const currentUserId = authStore.user?.id
      const settings = authStore.userSettings
      const contact = store.contacts.find(c => c.id === contactId)
      const assignedUserId = data.assignedUserId || contact?.assigned_user_id
      const isAssignedToUser = assignedUserId === currentUserId
      const alertsEnabled = settings.new_message_alerts !== false

      if (isAssignedToUser && alertsEnabled) {
        const senderName = data.profileName || contact?.profile_name || contact?.name || 'Unknown'
        const messagePreview = message.content?.body || 'New message'
        const preview = messagePreview.length > 50
          ? messagePreview.substring(0, 50) + '...'
          : messagePreview
        playNotificationSound()
        showNotification(senderName, preview, contactId)
      }
    }

    const alreadyRead = message.status === 'read'
    const userActive = typeof document === 'undefined'
      || (document.visibilityState === 'visible' && document.hasFocus())

    if (isViewingThisContact && currentContact && message.direction === 'incoming' && !alreadyRead && userActive) {
      contactsService.markRead(currentContact.id)
        .catch(() => { /* non-critical */ })
    }
  }

  private unsubscribeContacts() {
    if (this.contactsUnsubscribe) {
      this.contactsUnsubscribe()
      this.contactsUnsubscribe = null
    }
  }

  private unsubscribeMessages() {
    if (this.messagesUnsubscribe) {
      this.messagesUnsubscribe()
      this.messagesUnsubscribe = null
    }
  }
}

export const firestoreService = new FirestoreService()
