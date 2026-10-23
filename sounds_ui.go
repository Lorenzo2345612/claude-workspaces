package main

import "fmt"

func uiNotificationSounds(event string) error {
	if event != "question" && event != "completion" {
		return fmt.Errorf("evento de sonido desconocido")
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	current := cfg.QuestionSound
	title := "Sonido para preguntas y permisos"
	if event == "completion" {
		current = cfg.CompletionSound
		title = "Sonido de trabajo terminado"
	}
	ids := []string{"none"}
	for _, tone := range notificationTones {
		ids = append(ids, tone.ID)
	}
	selected := 0
	for i, id := range ids {
		if id == current {
			selected = i
		}
	}
	setRaw(true)
	defer func() { setRaw(false); stdoutWrite("\033[?25h") }()
	note := ""
	for {
		var s screen
		_, rows := termSize()
		visible := max(1, min(len(ids), rows-9))
		top := max(0, min(selected-visible+1, len(ids)-visible))
		s.at(1, 4, sgrBold+title+sgrReset)
		s.at(2, 4, sgrMuted+"↑↓ elegir · Espacio escuchar · Enter guardar · Esc cancelar"+sgrReset)
		for i := top; i < top+visible; i++ {
			style := sgrField
			if i == selected {
				style = sgrSel
			}
			s.btn(4+i-top, 4, style+" "+fit(soundLabel(ids[i]), 56)+" "+sgrReset, "tone:"+itoa(i))
		}
		row := 4 + visible
		s.at(row, 4, sgrMuted+"Volumen: "+itoa(cfg.SoundVolume)+" % · "+note+sgrReset)
		s.place(row+2, 4, []lineItem{buttonLine(0, btnDef{label: "Cancelar", id: "cancel"}, btnDef{label: "Escuchar", id: "preview"}, btnDef{label: "Guardar", id: "save", primary: true})})
		s.closeX("cancel")
		s.flush(true)
		keys, err := readKeys()
		if err != nil {
			return nil
		}
		for _, k := range keys {
			id := ""
			if k.kind == keyClick {
				id = hitAt(s.hits, k.x, k.y)
			}
			switch {
			case id == "cancel" || k.kind == keyEsc:
				return nil
			case len(id) > 5 && id[:5] == "tone:":
				selected = atoiDefault(id[5:], selected)
			case k.kind == keyDown || k.kind == keyWheelDown:
				selected = (selected + 1) % len(ids)
			case k.kind == keyUp || k.kind == keyWheelUp:
				selected = (selected - 1 + len(ids)) % len(ids)
			case id == "preview" || (k.kind == keyRune && k.r == ' '):
				if err := playNotification(ids[selected], cfg.SoundVolume); err != nil {
					note = err.Error()
				} else {
					note = soundLabel(ids[selected])
				}
			case id == "save" || k.kind == keyEnter:
				// Read again to preserve any other setting changed while this dialog was open.
				latest, err := loadConfig()
				if err != nil {
					return err
				}
				if event == "question" {
					latest.QuestionSound = ids[selected]
				} else {
					latest.CompletionSound = ids[selected]
				}
				return saveConfig(latest)
			}
		}
	}
}
