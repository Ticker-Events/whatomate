import type { Message, ReplyPreview } from '@/stores/contacts'

export type CarouselCard = {
  media_type?: string
  media_url?: string
  action?: string
  body?: string
  url?: string
  buttons?: Array<{ id?: string; title?: string }>
}

function contentBody(content: unknown): string {
  if (typeof content === 'string') return content
  if (content && typeof content === 'object') {
    const record = content as Record<string, unknown>
    if (typeof record.body === 'string') return record.body
    if (typeof record.text === 'string') return record.text
  }
  return ''
}

function firstLine(text: string): string {
  const trimmed = text.trim()
  if (!trimmed) return ''
  return trimmed.split('\n')[0]!.trim()
}

function truncatePreview(text: string, max = 80): string {
  const trimmed = text.trim()
  if (trimmed.length <= max) return trimmed
  return `${trimmed.slice(0, max - 1)}…`
}

export function isPublicMediaUrl(url: string | undefined): boolean {
  if (!url || typeof url !== 'string') return false
  return /^https?:\/\//i.test(url.trim())
}

export function getCarouselCards(message: Message): CarouselCard[] {
  const data = message.interactive_data as
    | { type?: string; cards?: CarouselCard[] }
    | undefined
  if (
    message.message_type !== 'interactive' ||
    data?.type !== 'carousel' ||
    !Array.isArray(data.cards)
  ) {
    return []
  }
  return data.cards.filter((card): card is CarouselCard => Boolean(card) && typeof card === 'object')
}

function replyButtonId(message: Message): string {
  const id = (message.interactive_data as any)?.button_id
  return typeof id === 'string' ? id.trim() : ''
}

function replyButtonTitle(message: Message): string {
  const title = (message.interactive_data as any)?.title
  if (typeof title === 'string' && title.trim()) return title.trim()
  return contentBody(message.content).trim()
}

/** Card the user tapped on a quoted carousel (by button id, or a saved snapshot). */
export function getMatchedCarouselCard(message: Message): CarouselCard | null {
  const data = message.interactive_data as any
  const snapshotBody = typeof data?.card_body === 'string' ? data.card_body.trim() : ''
  const snapshotMedia = typeof data?.card_media_url === 'string' ? data.card_media_url.trim() : ''
  if (snapshotBody || isPublicMediaUrl(snapshotMedia)) {
    return {
      body: snapshotBody || undefined,
      media_url: snapshotMedia || undefined,
      media_type: 'image',
    }
  }

  const quoted = message.reply_to_message
  if (!quoted || quoted.message_type !== 'interactive') return null
  const cards = getCarouselCards({
    message_type: 'interactive',
    interactive_data: quoted.interactive_data,
  } as Message)
  if (cards.length === 0) return null

  const buttonId = replyButtonId(message)
  if (buttonId) {
    const byId = cards.find(card => card.buttons?.some(button => button.id === buttonId))
    if (byId) return byId
  }

  const title = replyButtonTitle(message)
  if (!title) return null
  const byTitle = cards.filter(card => card.buttons?.some(button => button.title === title))
  return byTitle.length === 1 ? byTitle[0]! : null
}

export function getCarouselPreviewImage(interactiveData: any): string | undefined {
  if (interactiveData?.type !== 'carousel' || !Array.isArray(interactiveData.cards)) {
    return undefined
  }
  for (const card of interactiveData.cards) {
    const url = card?.media_url
    if (
      isPublicMediaUrl(url) &&
      String(card?.media_type || 'image').toLowerCase() !== 'video'
    ) {
      return String(url).trim()
    }
  }
  return undefined
}

export function getCarouselReplyPreviewText(interactiveData: any): string {
  if (!interactiveData || interactiveData.type !== 'carousel') return ''
  const body = typeof interactiveData.body === 'string' ? firstLine(interactiveData.body) : ''
  return truncatePreview(body || '[Carousel]')
}

export function buildReplyPreviewFromMessage(message: Message): ReplyPreview {
  return {
    id: message.id,
    content: message.content,
    message_type: message.message_type,
    direction: message.direction,
    interactive_data: message.interactive_data,
  }
}

export function resolveReplyPreviews(messages: Message[]): Message[] {
  const byId = new Map(messages.map((message) => [message.id, message]))
  return messages.map((message) => {
    const replyId = message.reply_to_message_id
    if (!replyId || message.reply_to_message) return message
    const quoted = byId.get(replyId)
    if (!quoted) return message
    return {
      ...message,
      is_reply: true,
      reply_to_message: buildReplyPreviewFromMessage(quoted),
    }
  })
}

export function replyPreviewTextFromMessage(message: Message): string {
  const matchedCard = getMatchedCarouselCard(message)
  if (matchedCard) {
    const cardLabel = firstLine(matchedCard.body || '')
    if (cardLabel) return truncatePreview(cardLabel)
  }
  if (!message.reply_to_message) return ''
  return replyPreviewTextFromQuote(message.reply_to_message)
}

export function replyPreviewImageFromMessage(message: Message): string | undefined {
  if (!message.reply_to_message) return undefined
  const matchedCard = getMatchedCarouselCard(message)
  if (matchedCard) {
    const url = matchedCard.media_url
    if (
      isPublicMediaUrl(url) &&
      String(matchedCard.media_type || 'image').toLowerCase() !== 'video'
    ) {
      return String(url).trim()
    }
    return undefined
  }
  if (message.message_type === 'button_reply') return undefined
  return getCarouselPreviewImage(message.reply_to_message.interactive_data)
}

export function replyPreviewTextFromQuote(reply: ReplyPreview): string {
  if (reply.message_type === 'interactive' && reply.interactive_data?.type === 'carousel') {
    return getCarouselReplyPreviewText(reply.interactive_data)
  }
  if (reply.message_type === 'text' || reply.message_type === 'button_reply') {
    return truncatePreview(contentBody(reply.content))
  }
  if (reply.message_type === 'interactive' || reply.message_type === 'template') {
    const body =
      (typeof reply.interactive_data?.body === 'string' && reply.interactive_data.body) ||
      contentBody(reply.content)
    return truncatePreview(body || '[Message]')
  }
  if (reply.message_type === 'image') return '[Photo]'
  if (reply.message_type === 'video') return '[Video]'
  if (reply.message_type === 'audio') return '[Audio]'
  if (reply.message_type === 'document') return '[Document]'
  if (reply.message_type === 'location') return '[Location]'
  if (reply.message_type === 'contacts') return '[Contact]'
  if (reply.message_type === 'sticker') return '[Sticker]'
  return truncatePreview(contentBody(reply.content) || '[Message]')
}
