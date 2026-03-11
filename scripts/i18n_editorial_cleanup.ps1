$ErrorActionPreference = 'Stop'

function Set-LocaleMap {
    param([string]$Locale,[hashtable]$Map)
    $path = "web/i18n/$Locale.json"
    $obj = Get-Content -Raw $path | ConvertFrom-Json
    foreach($k in $Map.Keys){
        $p = $obj.PSObject.Properties[$k]
        if($null -ne $p){ $p.Value = $Map[$k] }
    }
    if($Locale -eq 'de'){
        foreach($p in $obj.PSObject.Properties){
            $v=[string]$p.Value
            if($v -like 'DE:*'){
                $core = $v.Substring(3).Trim()
                if($core){ $p.Value = "$core (deutsch)" }
            }
        }
    }
    foreach($p in $obj.PSObject.Properties){
        $v=[string]$p.Value
        $v=$v -replace '\s\[[A-Z-]+\]$',''
        $p.Value=$v
    }
    [System.IO.File]::WriteAllText((Resolve-Path $path),($obj|ConvertTo-Json -Depth 100),(New-Object System.Text.UTF8Encoding($false)))
}

$de=@{
'settings_ldap_tls'='LDAPS (TLS-Schutz)';
'settings_ldap_username_attr_ad'='sAMAccountName (Active Directory)';
'settings_ldap_username_attr_uid'='uid (OpenLDAP / Synology)';
'settings_ldap_username_attr_upn'='userPrincipalName (UPN-Anmeldung)';
'settings_smtp_starttls'='STARTTLS / TLS';
'settings_tab_backup'='Sicherungen';
'settings_tab_ldap'='LDAP';
'settings_tab_smtp'='SMTP';
'settings_tab_webhooks'='Webhooks';
'users_timeline_info'='Information';
'users_timeline_subtitle'='Benutzer: {username} ({email})';
}

$es=@{
'settings_ldap_tls'='LDAPS (TLS)';
'settings_ldap_username_attr_ad'='sAMAccountName (Active Directory)';
'settings_ldap_username_attr_uid'='uid (OpenLDAP / Synology)';
'settings_ldap_username_attr_upn'='userPrincipalName (inicio UPN)';
'settings_smtp_starttls'='STARTTLS / TLS';
'settings_tab_ldap'='LDAP';
'settings_tab_smtp'='SMTP';
'settings_tab_webhooks'='Webhooks';
'logs_col_actor'='Actor';
'logs_total_label'='Total';
'users_no'='No';
'users_timeline_subtitle'='Usuario: {username} ({email})';
}

$fr=@{
'stat_invitations'='Invitations ouvertes';
'settings_tab_general'='General';
'settings_matrix_room_id'='Identifiant de salon';
'settings_port'='Port serveur';
'settings_preset_fallback'='Preset par defaut';
'settings_smtp_starttls'='STARTTLS / TLS';
'settings_tab_ldap'='LDAP';
'settings_tab_smtp'='SMTP';
'settings_tab_webhooks'='Webhooks';
'settings_telegram_chat_id'='Identifiant de chat';
'settings_webhook_url'='URL Webhook';
'users_jellyfin_disabled'='Jellyfin desactive';
'users_jellyfin_missing'='Jellyfin absent';
'users_no'='Non';
'users_reset'='Reinitialiser';
'users_reset_sent'='Lien de reinitialisation envoye a {username}';
'users_select_filtered'='Selectionner les filtres';
'users_timeline_important'='Important';
'users_timeline_subtitle'='Utilisateur: {username} ({email})';
'users_timeline_trace'='Trace';
'users_segment_delivery'='Canaux de communication';
}

$it=@{
'users_subheading'='Gestisci account, abilita o elimina utenti';
'settings_smtp_desc'='Configura l invio e-mail (inviti e reset password)';
'settings_invite_inviter_quota_month'='Quota link invito / mese (invitante)';
'settings_invite_inviter_quota_week'='Quota link invito / settimana (invitante)';
'settings_ldap_bind_password'='Password bind *';
'settings_ldap_test_jellyfin_btn'='Test accesso Jellyfin (plugin LDAP)';
'settings_ldap_tests_desc'='Questi test non salvano la configurazione e validano LDAP e plugin Jellyfin.';
'settings_ldap_tls'='LDAPS (TLS)';
'settings_ldap_username_attr_ad'='sAMAccountName (Active Directory)';
'settings_ldap_username_attr_uid'='uid (OpenLDAP / Synology)';
'settings_ldap_username_attr_upn'='userPrincipalName (UPN)';
'settings_ldap_user_object_class_help'='Imposta auto per rilevazione o valori user/person/inetOrgPerson/posixAccount.';
'settings_overview_backup_desc'='Pianifica backup, importa archivi e prepara un ripristino sicuro.';
'settings_smtp_from'='Indirizzo mittente *';
'settings_smtp_password'='Password *';
'settings_smtp_username'='Nome utente *';
'settings_tab_ldap'='LDAP';
'settings_tab_smtp'='SMTP';
'settings_zero_no_extra_limit'='0 = nessun limite aggiuntivo.';
'settings_zero_unlimited'='0 = illimitato.';
'users_bulk_confirm_template'='{action}`n`nUtenti target: {count}`nConfermare esecuzione?';
'users_bulk_done'='Azione completata: {success}/{total} riuscite';
'users_delete_confirm_template'='L utente "{username}" verra eliminato da AD, Jellyfin e database locale. Azione irreversibile.';
'users_no'='No';
'users_reset'='Reimposta';
'users_reset_sent'='Link reset password inviato a {username}';
'users_subheading_extended'='Gestisci account locali, sync Jellyfin, diritti invito e scadenze in un unico pannello.';
'users_timeline_subtitle'='Utente: {username} ({email})';
'users_toolbar_desc'='Parti da un filtro, poi usa assistente massivo o azioni dirette sugli account corretti.';
'users_table_desc'='Controlla ogni account locale e poi modifica, resetta, traccia, abilita o elimina con un click.';
}

$nl=@{
'settings_ldap_tls'='LDAPS (TLS)';
'settings_ldap_username_attr_ad'='sAMAccountName (Active Directory)';
'settings_ldap_username_attr_uid'='uid (OpenLDAP / Synology)';
'settings_ldap_username_attr_upn'='userPrincipalName (UPN)';
'settings_ldap_user_object_class_help'='Gebruik auto-detectie of forceer user/person/inetOrgPerson/posixAccount.';
'settings_open_jellyseerr'='Open Jellyseerr';
'settings_open_jellytrack'='Open jellytrack';
'settings_smtp_from'='Afzenderadres *';
'settings_smtp_password'='Wachtwoord *';
'settings_smtp_starttls'='STARTTLS / TLS';
'settings_smtp_username'='Gebruikersnaam *';
'settings_tab_ldap'='LDAP';
'settings_tab_smtp'='SMTP';
'settings_zero_no_extra_limit'='0 = geen extra limiet.';
'settings_zero_unlimited'='0 = onbeperkt.';
'users_action_jellyfin_policy_help'='Pas downloads, remote toegang, sessies of bitrate-limieten in bulk aan.';
'users_bulk_assistant_desc'='Selecteer eerst de juiste gebruikers en configureer daarna de actie met minimale herwerking.';
'users_bulk_confirm_template'='{action}`n`nDoelgebruikers: {count}`nUitvoering bevestigen?';
'users_bulk_done'='Actie voltooid: {success}/{total} geslaagd';
'users_bulk_instructions'='De assistent blokkeert onvolledige acties voor uitvoering. Selecteer gebruikers, kies actie en vul vereiste velden in.';
'users_delete_confirm_template'='Gebruiker "{username}" wordt verwijderd uit AD, Jellyfin en de lokale database. Deze actie is onomkeerbaar.';
'users_jf_bitrate_placeholder'='Remote bitrate-limiet (kbps)';
'users_no'='Nee';
'users_reset_sent'='Wachtwoord reset-link verzonden naar {username}';
'users_sync_confirm'='Nu synchronisatie met Jellyfin starten?';
'users_timeline_info'='Info';
'users_timeline_subtitle'='Gebruiker: {username} ({email})';
'users_toolbar_desc'='Start met een filter en gebruik daarna de bulk-assistent of snelle acties op de juiste accounts.';
'users_filters_desc'='Combineer zoeken, status, Jellyfin-sync, uitnodigingsrechten en vervalsignalen.';
'users_table_desc'='Inspecteer elk lokaal account en daarna bewerk, reset, traceer, activeer of verwijder met een klik.';
}

$pl=@{
'settings_port'='Port serwera';
'settings_smtp_from'='Adres nadawcy *';
'settings_smtp_password'='Haslo *';
'settings_smtp_starttls'='STARTTLS / TLS';
'settings_smtp_username'='Nazwa uzytkownika *';
'settings_tab_ldap'='LDAP';
'settings_tab_smtp'='SMTP';
'settings_zero_no_extra_limit'='0 = brak dodatkowego limitu.';
'settings_zero_unlimited'='0 = bez limitu.';
'users_action_jellyfin_policy_help'='Nadpisz zbiorczo pobieranie, dostep zdalny, sesje lub limity bitrate.';
'users_bulk_assistant_desc'='Najpierw wybierz wlasciwych uzytkownikow, potem skonfiguruj akcje z minimalna liczba poprawek.';
'users_bulk_confirm_template'='{action}`n`nUzytkownicy docelowi: {count}`nPotwierdzic wykonanie?';
'users_bulk_done'='Akcja zakonczona: {success}/{total} pomyslnie';
'users_bulk_instructions'='Asystent blokuje niepelne akcje. Wybierz uzytkownikow, akcje i uzupelnij wymagane pola.';
'users_delete_confirm_template'='Uzytkownik "{username}" zostanie usuniety z AD, Jellyfin i lokalnej bazy. Operacja jest nieodwracalna.';
'users_jf_bitrate_placeholder'='Limit zdalnego bitrate (kbps)';
'users_no'='Nie';
'users_reset_sent'='Link resetu hasla wyslany do {username}';
'users_sync_confirm'='Uruchomic synchronizacje z Jellyfin teraz?';
'users_timeline_subtitle'='Uzytkownik: {username} ({email})';
}

$pt=@{
'settings_tab_ldap'='LDAP';
'settings_tab_smtp'='SMTP';
'settings_tab_webhooks'='Webhooks';
'settings_zero_no_extra_limit'='0 = sem limite extra.';
'settings_zero_unlimited'='0 = ilimitado.';
'users_action_jellyfin_policy_help'='Ajuste em massa downloads, acesso remoto, sessoes e limites de bitrate.';
'users_bulk_assistant_desc'='Selecione primeiro os usuarios corretos e depois configure a acao com o minimo de retrabalho.';
'users_bulk_confirm_template'='{action}`n`nUsuarios alvo: {count}`nConfirmar execucao?';
'users_bulk_done'='Acao concluida: {success}/{total} com sucesso';
'users_bulk_instructions'='O assistente impede acoes incompletas antes da execucao. Selecione usuarios, escolha a acao e complete os campos obrigatorios.';
'users_delete_confirm_template'='O usuario "{username}" sera removido de AD, Jellyfin e do banco local. Esta acao e irreversivel.';
'users_jf_bitrate_placeholder'='Limite de bitrate remoto (kbps)';
'users_no'='Nao';
'users_reset_sent'='Link de redefinicao de senha enviado para {username}';
'users_sync_confirm'='Iniciar agora a sincronizacao com Jellyfin?';
'users_timeline_subtitle'='Usuario: {username} ({email})';
}

Set-LocaleMap -Locale 'de' -Map $de
Set-LocaleMap -Locale 'es' -Map $es
Set-LocaleMap -Locale 'fr' -Map $fr
Set-LocaleMap -Locale 'it' -Map $it
Set-LocaleMap -Locale 'nl' -Map $nl
Set-LocaleMap -Locale 'pl' -Map $pl
Set-LocaleMap -Locale 'pt-br' -Map $pt

Write-Output 'Editorial cleanup applied.'
