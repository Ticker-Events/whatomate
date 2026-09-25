<script setup lang="ts">
import { PageHeader } from '@/components/shared'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { ScrollArea } from '@/components/ui/scroll-area'
import { BookOpen, Key, FileText, Bot } from 'lucide-vue-next'
</script>

<template>
  <div class="flex flex-col h-full bg-[#0a0a0b] light:bg-gray-50">
    <PageHeader
      :title="$t('workflowsGuide.title')"
      :description="$t('workflowsGuide.subtitle')"
      :icon="BookOpen"
      icon-gradient="bg-gradient-to-br from-emerald-500 to-teal-600 shadow-emerald-500/20"
    />

    <ScrollArea class="flex-1">
      <div class="p-6 max-w-3xl mx-auto space-y-8">
        <p class="text-sm text-muted-foreground">
          {{ $t('workflowsGuide.intro') }}
        </p>

        <div class="grid gap-3 sm:grid-cols-3">
          <a href="#api-keys" class="block rounded-lg border border-white/[0.08] light:border-gray-200 p-4 hover:bg-white/[0.04] light:hover:bg-gray-100 transition-colors no-underline text-inherit">
            <div class="flex items-center gap-2 font-medium mb-1">
              <Key class="h-4 w-4 text-amber-500" />
              {{ $t('workflowsGuide.cardApiKeys') }}
            </div>
            <p class="text-xs text-muted-foreground">{{ $t('workflowsGuide.cardApiKeysDesc') }}</p>
          </a>
          <a href="#templates" class="block rounded-lg border border-white/[0.08] light:border-gray-200 p-4 hover:bg-white/[0.04] light:hover:bg-gray-100 transition-colors no-underline text-inherit">
            <div class="flex items-center gap-2 font-medium mb-1">
              <FileText class="h-4 w-4 text-blue-500" />
              {{ $t('workflowsGuide.cardTemplates') }}
            </div>
            <p class="text-xs text-muted-foreground">{{ $t('workflowsGuide.cardTemplatesDesc') }}</p>
          </a>
          <a href="#chatbot" class="block rounded-lg border border-white/[0.08] light:border-gray-200 p-4 hover:bg-white/[0.04] light:hover:bg-gray-100 transition-colors no-underline text-inherit">
            <div class="flex items-center gap-2 font-medium mb-1">
              <Bot class="h-4 w-4 text-purple-500" />
              {{ $t('workflowsGuide.cardChatbot') }}
            </div>
            <p class="text-xs text-muted-foreground">{{ $t('workflowsGuide.cardChatbotDesc') }}</p>
          </a>
        </div>

        <!-- API Keys -->
        <Card id="api-keys">
          <CardHeader>
            <CardTitle>{{ $t('workflowsGuide.apiKeysTitle') }}</CardTitle>
            <CardDescription>{{ $t('workflowsGuide.apiKeysDesc') }}</CardDescription>
          </CardHeader>
          <CardContent class="space-y-4 text-sm">
            <ol class="list-decimal list-inside space-y-3 text-muted-foreground">
              <li>
                <span class="text-foreground font-medium">{{ $t('workflowsGuide.apiKeysStep1Title') }}</span>
                <p class="ml-5 mt-1">
                  {{ $t('workflowsGuide.apiKeysStep1Body') }}
                  <RouterLink to="/settings/api-keys" class="text-primary underline-offset-4 hover:underline">/settings/api-keys</RouterLink>.
                </p>
              </li>
              <li>
                <span class="text-foreground font-medium">{{ $t('workflowsGuide.apiKeysStep2Title') }}</span>
                <p class="ml-5 mt-1">{{ $t('workflowsGuide.apiKeysStep2Body') }}</p>
              </li>
              <li>
                <span class="text-foreground font-medium">{{ $t('workflowsGuide.apiKeysStep3Title') }}</span>
                <p class="ml-5 mt-1">{{ $t('workflowsGuide.apiKeysStep3Body') }}</p>
              </li>
              <li>
                <span class="text-foreground font-medium">{{ $t('workflowsGuide.apiKeysStep4Title') }}</span>
                <pre class="ml-5 mt-2 overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>curl -X GET "http://your-server:8080/api/contacts" \
  -H "X-API-Key: whm_your_api_key_here"</code></pre>
              </li>
            </ol>
            <Alert>
              <AlertDescription>{{ $t('workflowsGuide.apiKeysNote') }}</AlertDescription>
            </Alert>
          </CardContent>
        </Card>

        <!-- Templates -->
        <Card id="templates">
          <CardHeader>
            <CardTitle>{{ $t('workflowsGuide.templatesTitle') }}</CardTitle>
            <CardDescription>{{ $t('workflowsGuide.templatesDesc') }}</CardDescription>
          </CardHeader>
          <CardContent class="space-y-6 text-sm">
            <div>
              <h3 class="font-medium mb-3">{{ $t('workflowsGuide.templatesCreateHeading') }}</h3>
              <ol class="list-decimal list-inside space-y-3 text-muted-foreground">
                <li>
                  <span class="text-foreground font-medium">{{ $t('workflowsGuide.templatesStep1Title') }}</span>
                  <p class="ml-5 mt-1">
                    {{ $t('workflowsGuide.templatesStep1Body') }}
                    <RouterLink to="/templates/new" class="text-primary underline-offset-4 hover:underline">/templates/new</RouterLink>.
                  </p>
                </li>
                <li>
                  <span class="text-foreground font-medium">{{ $t('workflowsGuide.templatesStep2Title') }}</span>
                  <p class="ml-5 mt-1">{{ $t('workflowsGuide.templatesStep2Body') }}</p>
                </li>
                <li>
                  <span class="text-foreground font-medium">{{ $t('workflowsGuide.templatesStep3Title') }}</span>
                  <p class="ml-5 mt-1">{{ $t('workflowsGuide.templatesStep3Body') }}</p>
                </li>
                <li>
                  <span class="text-foreground font-medium">{{ $t('workflowsGuide.templatesStep4Title') }}</span>
                  <p class="ml-5 mt-1">
                    {{ $t('workflowsGuide.templatesStep4Body') }}
                    <RouterLink to="/templates" class="text-primary underline-offset-4 hover:underline">{{ $t('nav.templates') }}</RouterLink>.
                  </p>
                </li>
                <li>
                  <span class="text-foreground font-medium">{{ $t('workflowsGuide.templatesStep5Title') }}</span>
                  <p class="ml-5 mt-1">{{ $t('workflowsGuide.templatesStep5Body') }}</p>
                </li>
              </ol>
              <p class="mt-3 text-muted-foreground">{{ $t('workflowsGuide.templatesLifecycle') }}</p>
            </div>

            <div>
              <h3 class="font-medium mb-3">{{ $t('workflowsGuide.templatesSendHeading') }}</h3>
              <p class="text-muted-foreground mb-2">{{ $t('workflowsGuide.templatesSendIntro') }}</p>
              <p class="text-muted-foreground mb-2">
                <span class="text-foreground font-medium">{{ $t('workflowsGuide.templatesSendUiLabel') }}</span>
                {{ $t('workflowsGuide.templatesSendUiBody') }}
                <RouterLink to="/chat" class="text-primary underline-offset-4 hover:underline">{{ $t('nav.chat') }}</RouterLink>.
              </p>
              <p class="text-muted-foreground mb-2">
                <span class="text-foreground font-medium">{{ $t('workflowsGuide.templatesSendApiLabel') }}</span>
              </p>
              <pre class="overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>curl -X POST "http://your-server:8080/api/messages/template" \
  -H "X-API-Key: whm_your_api_key_here" \
  -H "Content-Type: application/json" \
  -d '{
    "phone_number": "919876543210",
    "template_name": "hello_world"
  }'</code></pre>
              <p class="mt-3 text-muted-foreground mb-2">{{ $t('workflowsGuide.templatesSendNamed') }}</p>
              <pre class="overflow-x-auto rounded-md bg-muted p-3 text-xs"><code>{
  "phone_number": "919876543210",
  "template_name": "order_confirmation",
  "template_params": {
    "name": "John",
    "order_id": "12345"
  }
}</code></pre>
              <p class="mt-3 text-muted-foreground">{{ $t('workflowsGuide.templatesSendExtra') }}</p>
              <Alert class="mt-3">
                <AlertDescription>
                  {{ $t('workflowsGuide.templatesCampaignTip') }}
                  <RouterLink to="/campaigns" class="text-primary underline-offset-4 hover:underline">{{ $t('nav.campaigns') }}</RouterLink>.
                </AlertDescription>
              </Alert>
            </div>
          </CardContent>
        </Card>

        <!-- Chatbot -->
        <Card id="chatbot">
          <CardHeader>
            <CardTitle>{{ $t('workflowsGuide.chatbotTitle') }}</CardTitle>
            <CardDescription>{{ $t('workflowsGuide.chatbotDesc') }}</CardDescription>
          </CardHeader>
          <CardContent class="space-y-4 text-sm">
            <ol class="list-decimal list-inside space-y-3 text-muted-foreground">
              <li>
                <span class="text-foreground font-medium">{{ $t('workflowsGuide.chatbotStep1Title') }}</span>
                <p class="ml-5 mt-1">
                  {{ $t('workflowsGuide.chatbotStep1Body') }}
                  <RouterLink to="/chatbot" class="text-primary underline-offset-4 hover:underline">/chatbot</RouterLink>.
                </p>
              </li>
              <li>
                <span class="text-foreground font-medium">{{ $t('workflowsGuide.chatbotStep2Title') }}</span>
                <p class="ml-5 mt-1">
                  {{ $t('workflowsGuide.chatbotStep2Body') }}
                  <RouterLink to="/settings/chatbot" class="text-primary underline-offset-4 hover:underline">/settings/chatbot</RouterLink>.
                </p>
              </li>
              <li>
                <span class="text-foreground font-medium">{{ $t('workflowsGuide.chatbotStep3Title') }}</span>
                <p class="ml-5 mt-1">
                  {{ $t('workflowsGuide.chatbotStep3Body') }}
                  <RouterLink to="/chatbot/flows/new" class="text-primary underline-offset-4 hover:underline">/chatbot/flows/new</RouterLink>.
                </p>
              </li>
              <li>
                <span class="text-foreground font-medium">{{ $t('workflowsGuide.chatbotStep4Title') }}</span>
                <p class="ml-5 mt-1">{{ $t('workflowsGuide.chatbotStep4Body') }}</p>
              </li>
              <li>
                <span class="text-foreground font-medium">{{ $t('workflowsGuide.chatbotStep5Title') }}</span>
                <p class="ml-5 mt-1">
                  {{ $t('workflowsGuide.chatbotStep5Body') }}
                  <RouterLink to="/chatbot/flows" class="text-primary underline-offset-4 hover:underline">{{ $t('nav.flows') }}</RouterLink>.
                </p>
              </li>
              <li>
                <span class="text-foreground font-medium">{{ $t('workflowsGuide.chatbotStep6Title') }}</span>
                <p class="ml-5 mt-1">{{ $t('workflowsGuide.chatbotStep6Body') }}</p>
              </li>
              <li>
                <span class="text-foreground font-medium">{{ $t('workflowsGuide.chatbotStep7Title') }}</span>
                <p class="ml-5 mt-1">{{ $t('workflowsGuide.chatbotStep7Body') }}</p>
              </li>
            </ol>
            <Alert>
              <AlertDescription>{{ $t('workflowsGuide.chatbotNote') }}</AlertDescription>
            </Alert>
            <p class="text-muted-foreground">
              {{ $t('workflowsGuide.chatbotVsWaFlows') }}
              <RouterLink to="/flows" class="text-primary underline-offset-4 hover:underline">/flows</RouterLink>.
            </p>
          </CardContent>
        </Card>
      </div>
    </ScrollArea>
  </div>
</template>
